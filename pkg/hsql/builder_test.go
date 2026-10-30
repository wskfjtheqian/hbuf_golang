package hsql

import (
	"context"
	"database/sql/driver"
	"regexp"
	"testing"
	"time"
)

// 测试 InsertAction.String 方法
func TestInsertActionString(t *testing.T) {
	t.Run("Insert", func(t *testing.T) {
		if got := Insert.String(); got != "INSERT INTO" {
			t.Errorf("期望 INSERT INTO, 得到 %q", got)
		}
	})
	t.Run("Replace", func(t *testing.T) {
		if got := Replace.String(); got != "REPLACE INTO" {
			t.Errorf("期望 REPLACE INTO, 得到 %q", got)
		}
	})
	t.Run("Ignore", func(t *testing.T) {
		if got := Ignore.String(); got != "INSERT IGNORE INTO" {
			t.Errorf("期望 INSERT IGNORE INTO, 得到 %q", got)
		}
	})
	t.Run("未知值回退默认", func(t *testing.T) {
		if got := InsertAction(99).String(); got != "INSERT INTO" {
			t.Errorf("期望默认 INSERT INTO, 得到 %q", got)
		}
	})
}

// 测试 isPrintable 函数
func TestIsPrintable(t *testing.T) {
	t.Run("全部可打印", func(t *testing.T) {
		if !isPrintable("abc 123!,.") {
			t.Error("可打印字符串应返回 true")
		}
	})
	t.Run("包含控制字符", func(t *testing.T) {
		if isPrintable("ab\nc") {
			t.Error("含换行符应返回 false")
		}
	})
	t.Run("空字符串", func(t *testing.T) {
		if !isPrintable("") {
			t.Error("空字符串应返回 true")
		}
	})
}

// 测试 ToString 函数
func TestToString(t *testing.T) {
	cases := []struct {
		name  string
		input any
		want  string
	}{
		{"string", "hello", "hello"},
		{"int", 123, "123"},
		{"int8", int8(8), "8"},
		{"int16", int16(16), "16"},
		{"int32", int32(32), "32"},
		{"int64", int64(64), "64"},
		{"uint", uint(7), "7"},
		{"uint8", uint8(8), "8"},
		{"uint16", uint16(16), "16"},
		{"uint32", uint32(32), "32"},
		{"uint64", uint64(64), "64"},
		{"不支持的类型返回空", 3.14, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ToString(c.input); got != c.want {
				t.Errorf("期望 %q, 得到 %q", c.want, got)
			}
		})
	}
}

// 测试 NewBuilder 与 IsEmpty
func TestNewBuilderAndIsEmpty(t *testing.T) {
	t.Run("初始为空", func(t *testing.T) {
		b := NewBuilder()
		if b == nil {
			t.Fatal("NewBuilder 不应返回 nil")
		}
		if !b.IsEmpty() {
			t.Error("新建 Builder 应为空")
		}
	})
	t.Run("写入后非空", func(t *testing.T) {
		b := NewBuilder()
		b.T("SELECT 1")
		if b.IsEmpty() {
			t.Error("写入文本后不应为空")
		}
	})
}

// 测试 Builder.T 文本写入
func TestBuilderT(t *testing.T) {
	b := NewBuilder()
	b.T("   SELECT   ")
	want := "SELECT "
	if got := b.text.String(); got != want {
		t.Errorf("期望 %q, 得到 %q", want, got)
	}
}

// 测试 Builder.V 与 Builder.P 参数写入
func TestBuilderVAndP(t *testing.T) {
	t.Run("V 写入占位符与参数", func(t *testing.T) {
		b := NewBuilder()
		b.V(42)
		if b.text.String() != "? " {
			t.Errorf("期望 %q, 得到 %q", "? ", b.text.String())
		}
		if len(b.params) != 1 || b.params[0] != 42 {
			t.Errorf("参数应为 [42], 得到 %v", b.params)
		}
	})
	t.Run("P 追加多个参数", func(t *testing.T) {
		b := NewBuilder()
		b.P(1, 2, 3)
		if len(b.params) != 3 {
			t.Errorf("期望 3 个参数, 得到 %d", len(b.params))
		}
	})
}

// 测试 Builder.L 参数列表
func TestBuilderL(t *testing.T) {
	b := NewBuilder()
	b.L(",", 1, 2, 3)
	want := "? ,? ,? "
	if got := b.text.String(); got != want {
		t.Errorf("期望 %q, 得到 %q", want, got)
	}
	if len(b.params) != 3 {
		t.Errorf("期望 3 个参数, 得到 %d", len(b.params))
	}
}

// 测试 Builder.removeStart 与 Del
func TestBuilderRemoveStart(t *testing.T) {
	t.Run("移除起始分隔符并清空 del", func(t *testing.T) {
		b := NewBuilder()
		b.del = ","
		if got := b.removeStart(",abc"); got != "abc" {
			t.Errorf("期望 %q, 得到 %q", "abc", got)
		}
		if b.del != "" {
			t.Errorf("调用后 del 应被清空, 得到 %q", b.del)
		}
	})
	t.Run("无分隔符时原样返回", func(t *testing.T) {
		b := NewBuilder()
		if got := b.removeStart("abc"); got != "abc" {
			t.Errorf("期望 %q, 得到 %q", "abc", got)
		}
	})
	t.Run("分隔符不匹配前缀", func(t *testing.T) {
		b := NewBuilder()
		b.del = ","
		if got := b.removeStart("abc"); got != "abc" {
			t.Errorf("期望 %q, 得到 %q", "abc", got)
		}
	})
}

// 测试 Builder.Join
func TestBuilderJoin(t *testing.T) {
	t.Run("拼接 nil 返回自身", func(t *testing.T) {
		b := NewBuilder()
		b.T("A")
		got := b.Join(nil)
		if got != b {
			t.Error("Join(nil) 应返回自身")
		}
	})
	t.Run("合并文本与参数", func(t *testing.T) {
		b1 := NewBuilder()
		b1.T("SELECT ").V(1)
		b2 := NewBuilder()
		b2.T("WHERE a=").V(2)
		b1.Join(b2)
		want := "SELECT ? WHERE a=? "
		if got := b1.text.String(); got != want {
			t.Errorf("期望 %q, 得到 %q", want, got)
		}
		if len(b1.params) != 2 {
			t.Errorf("期望 2 个参数, 得到 %d", len(b1.params))
		}
	})
}

// 测试 Builder.ToText
func TestBuilderToText(t *testing.T) {
	b := NewBuilder()
	b.T("SELECT * FROM t WHERE id=").V(10)
	got := b.ToText(context.Background())
	want := "SELECT * FROM t WHERE id= 10 "
	if got != want {
		t.Errorf("期望 %q, 得到 %q", want, got)
	}
}

// 测试 ExplainSQL 各类参数渲染
func TestExplainSQL(t *testing.T) {
	loc := time.UTC
	normal := time.Date(2026, 10, 26, 12, 34, 56, 789000000, loc)
	var zeroTime time.Time

	cases := []struct {
		name  string
		sql   string
		fmt   string
		input any
		want  string
	}{
		{"字符串", "a=?", tmFmtWithMS, "hello", "a='hello'"},
		{"字符串转义单引号", "a=?", tmFmtWithMS, "it's", "a='it\\'s'"},
		{"布尔", "a=?", tmFmtWithMS, true, "a=true"},
		{"整数", "a=?", tmFmtWithMS, 123, "a=123"},
		{"浮点数", "a=?", tmFmtWithMS, 3.14, "a=3.140000"},
		{"字节切片可打印", "a=?", tmFmtWithMS, []byte("abc"), "a='abc'"},
		{"字节切片二进制", "a=?", tmFmtWithMS, []byte{0x01, 0x02}, "a='<binary>'"},
		{"nil 参数", "a=?", tmFmtWithMS, nil, "a=NULL"},
		{"时间零值", "a=?", tmFmtWithMS, zeroTime, "a='" + tmFmtZero + "'"},
		{"时间毫秒格式", "a=?", tmFmtWithMS, normal, "a='2026-10-26 12:34:56.789'"},
		{"时间秒格式", "a=?", tmFmtWithS, normal, "a='2026-10-26 12:34:56'"},
		{"时间指针空", "a=?", tmFmtWithMS, (*time.Time)(nil), "a=NULL"},
		{"时间指针零值", "a=?", tmFmtWithMS, &zeroTime, "a='" + tmFmtZero + "'"},
		{"指针解引用", "a=?", tmFmtWithMS, func() *int { v := 5; return &v }(), "a=5"},
		{"命名布尔类型可转换", "a=?", tmFmtWithMS, myBool(true), "a=true"},
		{"命名字符串类型可转换", "a=?", tmFmtWithMS, myString("hi"), "a='hi'"},
		{"Valuer 接口", "a=?", tmFmtWithMS, testValuer{v: "abc"}, "a='abc'"},
		{"Stringer 结构体", "a=?", tmFmtWithMS, testStringer{s: "hello"}, "a='hello'"},
		{"Stringer 整数底层类型", "a=?", tmFmtWithMS, intStringer(5), "a=5"},
		{"Stringer 字符串底层类型", "a=?", tmFmtWithMS, stringerString("abc"), "a='X'"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ExplainSQL(c.sql, nil, c.fmt, `'`, c.input)
			if got != c.want {
				t.Errorf("期望 %q, 得到 %q", c.want, got)
			}
		})
	}
}

// 测试 ExplainSQL 多参数占位符替换
func TestExplainSQLMultipleParams(t *testing.T) {
	got := ExplainSQL("a=? AND b=?", nil, tmFmtWithMS, `'`, 1, "x")
	want := "a=1 AND b='x'"
	if got != want {
		t.Errorf("期望 %q, 得到 %q", want, got)
	}
}

// 测试 ExplainSQL 占位符数量少于参数数量时保留剩余问号
func TestExplainSQLPlaceholderShort(t *testing.T) {
	got := ExplainSQL("a=?", nil, tmFmtWithMS, `'`, 1, 2)
	want := "a=1"
	if got != want {
		t.Errorf("期望 %q, 得到 %q", want, got)
	}
}

// 测试 ExplainSQL 数字占位符模式
func TestExplainSQLNumericPlaceholder(t *testing.T) {
	re := regexp.MustCompile(`\$(\d+)`)
	got := ExplainSQL("a=$1 AND b=$2", re, tmFmtWithMS, `'`, 10, "y")
	want := "a=10 AND b='y'"
	if got != want {
		t.Errorf("期望 %q, 得到 %q", want, got)
	}
}

// 测试 newPrintLog 与 printLog.print 不产生 panic
func TestPrintLog(t *testing.T) {
	ctx := context.Background()

	t.Run("关闭 PrintSQL 时不记录", func(t *testing.T) {
		old := PrintSQL
		PrintSQL = false
		defer func() { PrintSQL = old }()

		b := NewBuilder()
		b.T("SELECT 1")
		var count int64 = 3
		p := newPrintLog(b, &count)
		if !p.now.IsZero() {
			t.Error("PrintSQL 关闭时 now 应为零值")
		}
		// 关闭状态下 print 应直接返回, 不 panic
		p.print(ctx, nil)
	})

	t.Run("开启 PrintSQL 时正常输出", func(t *testing.T) {
		old := PrintSQL
		PrintSQL = true
		defer func() { PrintSQL = old }()

		b := NewBuilder()
		b.T("SELECT * FROM t WHERE id=").V(1)
		var count int64 = 1
		p := newPrintLog(b, &count)
		// cfg 为 nil 时应使用默认时间格式且不 panic
		p.print(ctx, nil)
	})
}

// 以下为测试辅助类型

type myBool bool

type myString string

type testValuer struct {
	v driver.Value
}

func (t testValuer) Value() (driver.Value, error) { return t.v, nil }

type testStringer struct {
	s string
}

func (t testStringer) String() string { return t.s }

type intStringer int

func (i intStringer) String() string { return "num" }

type stringerString string

func (s stringerString) String() string { return "X" }
