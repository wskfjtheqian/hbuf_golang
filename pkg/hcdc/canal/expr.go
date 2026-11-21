package canal

import (
	"io"
	"strconv"
	"strings"

	"github.com/pingcap/tidb/pkg/parser/ast"
	"github.com/pingcap/tidb/pkg/parser/charset"
	"github.com/pingcap/tidb/pkg/parser/format"
	"github.com/pingcap/tidb/pkg/parser/mysql"
	"github.com/pingcap/tidb/pkg/parser/types"
)

// 官方 parser 把 ast 包的字面量工厂（NewValueExpr/NewParamMarkerExpr/NewDecimal/
// NewHexLiteral/NewBitLiteral）声明为无实现的函数指针，由使用方注入（官方 tidb
// 主库与 test_driver 均自行注入）。这里注入「带值」的真实现：DDL 中的字面量
// （DEFAULT 值、COMMENT、分区边界等）必须能从 AST 还原 SQL 文本，供 hcdc 的
// columnToInfo 提取 DEFAULT/COMMENT 使用。
// 只做文本还原，不实现数值运算——canal/hcdc 不消费字面量的数值语义。
func init() {
	ast.NewValueExpr = newValueExpr
	ast.NewParamMarkerExpr = newParamExpr
	// 十进制/十六进制/二进制字面量包装为命名类型，newValueExpr 识别后直接输出
	// SQL 文本（lexer 传入的 str 为含前缀的完整原文，如 0x1A/b'101'，见
	// lexer.go startWithXx/startWithBb）。
	ast.NewDecimal = func(str string) (any, error) {
		return decimalLit(str), nil
	}
	ast.NewHexLiteral = func(str string) (any, error) {
		return hexLit(str), nil
	}
	ast.NewBitLiteral = func(str string) (any, error) {
		return bitLit(str), nil
	}
}

type decimalLit string // 十进制字面量文本（如 1.5）
type hexLit string     // 十六进制字面量完整 SQL 文本（如 0x1A）
type bitLit string     // 二进制字面量完整 SQL 文本（如 b'101'）

// valueExpr 保存字面量的 SQL 文本表示，Restore 时原样输出（字符串自动加引号）。
type valueExpr struct {
	ast.TexprNode
	text  string // 字面量文本；isStr 为 true 时是去引号的字符串内容
	isStr bool   // 字符串字面量，Restore 走 WriteString（自动加引号并转义）
}

func newValueExpr(value any, charsetName, collateName string) ast.ValueExpr {
	ve := &valueExpr{}
	ve.SetValue(value)
	ve.SetType(types.NewFieldType(mysql.TypeUnspecified))
	return ve
}

// SetValue 按 lexer 传入的字面量值类型（见 yy_parser.go）还原 SQL 文本。
func (ve *valueExpr) SetValue(val any) {
	ve.isStr = false
	switch x := val.(type) {
	case nil:
		ve.text = "NULL"
	case bool:
		if x {
			ve.text = "TRUE"
		} else {
			ve.text = "FALSE"
		}
	case int:
		ve.text = strconv.Itoa(x)
	case int64:
		ve.text = strconv.FormatInt(x, 10)
	case uint64:
		ve.text = strconv.FormatUint(x, 10)
	case float32:
		ve.text = strconv.FormatFloat(float64(x), 'e', -1, 32)
	case float64:
		ve.text = strconv.FormatFloat(x, 'e', -1, 64)
	case string:
		ve.isStr = true
		ve.text = x
	case decimalLit:
		ve.text = string(x)
	case hexLit:
		ve.text = string(x)
	case bitLit:
		ve.text = string(x)
	case ast.ValueExpr:
		// parser.y 的 SignedLiteral: Literal 规则会把内层 ValueExpr 二次包装
		// （官方 parser.y L4542-4544），DEFAULT 字面量都走这条路径，直接继承
		// 内层的文本表示，否则外层会得到空文本。
		if inner, ok := x.(*valueExpr); ok {
			ve.text, ve.isStr = inner.text, inner.isStr
			return
		}
		var sb strings.Builder
		_ = x.Restore(format.NewRestoreCtx(format.DefaultRestoreFlags, &sb))
		ve.text = sb.String()
	}
}

func (ve *valueExpr) GetValue() any          { return ve.text }
func (ve *valueExpr) GetDatumString() string { return ve.text }
func (ve *valueExpr) GetString() string      { return ve.text }

func (ve *valueExpr) GetProjectionOffset() int       { return 0 }
func (ve *valueExpr) SetProjectionOffset(offset int) {}

func (ve *valueExpr) Restore(ctx *format.RestoreCtx) error {
	if ve.isStr {
		// WriteString 自动加单引号并转义（RestoreStringSingleQuotes 默认开启）。
		ctx.WriteString(ve.text)
	} else {
		ctx.WritePlain(ve.text)
	}
	return nil
}

func (ve *valueExpr) Accept(v ast.Visitor) (node ast.Node, ok bool) { return node, ok }
func (ve *valueExpr) Text() string                                  { return ve.text }
func (ve *valueExpr) SetText(enc charset.Encoding, text string)     { ve.text = text }
func (ve *valueExpr) Format(w io.Writer) {
	_, _ = io.WriteString(w, ve.text)
}

// paramExpr 是 PREPARE 语句的 ? 占位符（DDL 场景不涉及，仅保持接口完整）。
type paramExpr struct {
	valueExpr
}

func newParamExpr(_ int) ast.ParamMarkerExpr {
	return &paramExpr{}
}

func (pe *paramExpr) SetOrder(o int) {}

func (pe *paramExpr) Restore(ctx *format.RestoreCtx) error {
	ctx.WritePlain("?")
	return nil
}
