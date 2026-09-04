package hcdc

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hutl"
)

func TestWorker_SchemaChange_And_HeaderRotation(t *testing.T) {
	// 1. 初始化一个临时的测试目录
	tmpDir, err := os.MkdirTemp("", "hcdc_test_*")
	if err != nil {
		t.Fatalf("创建临时目录失败: %v", err)
	}
	defer os.RemoveAll(tmpDir) // 测试完成后自动清理磁盘

	// 2. 构造测试数据和 Worker
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	schema := Schema("test_db")
	table := Table("user_order")

	worker := NewWorker(schema, table, tmpDir)

	// 启动后台消费协程
	go worker.loop(ctx)

	// ==========================================
	// 场景一：写入第一批数据（初始结构：id, user_id, amount）
	// ==========================================
	colsV1 := []Column{"id", "user_id", "amount"}
	rowsV1 := [][]RawBytes{
		{RawBytes("1001"), RawBytes("888"), RawBytes("99.5")},
	}

	err = worker.AddData(ctx, colsV1, rowsV1)
	if err != nil {
		t.Fatalf("第一次写入数据失败: %v", err)
	}

	// ==========================================
	// 场景二：表结构发生变更，新增了 status 列（新结构：id, user_id, amount, status）
	// ==========================================
	colsV2 := []Column{"id", "user_id", "amount", "status"}
	rowsV2 := [][]RawBytes{
		{RawBytes("1002"), RawBytes("999"), RawBytes("150.0"), RawBytes("SUCCESS")},
	}

	// 传入新 schema，此时内部应当触发自动切片（Rotate）
	err = worker.AddData(ctx, colsV2, rowsV2)
	if err != nil {
		t.Fatalf("第二次写入变更结构数据失败: %v", err)
	}

	// 等待落盘
	time.Sleep(10 * time.Second)

	// ==========================================
	// 验证阶段二：扫描并解析生成的 .active 文件
	// ==========================================
	var scannedFilesCount = 0
	var fileHeaders []string
	var fileContents []string

	// 调用我们重写过的 readFile 函数
	err = worker.readFile(ctx, func(ctx context.Context, fileColumns string, reader io.Reader) error {
		scannedFilesCount++
		// 记录解析出来的 Header
		fileHeaders = append(fileHeaders, fileColumns)

		// 读取剥离了第一行后的纯数据内容
		buf := new(strings.Builder)
		_, _ = io.Copy(buf, reader)
		fileContents = append(fileContents, buf.String())
		return nil
	})

	if err != nil {
		t.Fatalf("读取并解析 active 文件失败: %v", err)
	}

	// 1. 验证是否成功分裂成了 2 个独立文件（因为发生了 Schema Change 触发了主动切片）
	if scannedFilesCount != 2 {
		t.Errorf("预期生成 2 个文件，实际生成了 %d 个", scannedFilesCount)
	}

	// 2. 验证第一个 V1 文件的 Header 与数据内容
	expectedHeaderV1 := "id,user_id,amount,source_schema,source_table,instance_tag"
	if !contains(fileHeaders, expectedHeaderV1) {
		t.Errorf("未找到预期的 V1 文件头。\n预期包含: %s\n实际抓取到: %v", expectedHeaderV1, fileHeaders)
	}
	expectedDataV1 := "1001,888,99.5,test_db,user_order,1\n"
	if !contains(fileContents, expectedDataV1) {
		t.Errorf("未找到预期的 V1 纯数据内容。\n预期包含: %s\n实际抓取到: %v", expectedDataV1, fileContents)
	}

	// 3. 验证第二个 V2 文件的 Header 与数据内容
	expectedHeaderV2 := "id,user_id,amount,status,source_schema,source_table,instance_tag"
	if !contains(fileHeaders, expectedHeaderV2) {
		t.Errorf("未找到预期的 V2 文件头。\n预期包含: %s\n实际抓取到: %v", expectedHeaderV2, fileHeaders)
	}
	expectedDataV2 := "1002,999,150.0,SUCCESS,test_db,user_order,1\n"
	if !contains(fileContents, expectedDataV2) {
		t.Errorf("未找到预期的 V2 纯数据内容。\n预期包含: %s\n实际抓取到: %v", expectedDataV2, fileContents)
	}

	t.Logf("✅ 测试通过！成功验证 Schema 变更时文件自动切分、首行 Header 精准写入、以及无损剥离数据流功能。")
}

// 辅助工具函数：检查 slice 中是否包含目标字符串
func contains(slice []string, target string) bool {
	for _, item := range slice {
		if item == target {
			return true
		}
	}
	return false
}

func Test_CanalGetDatabase(t *testing.T) {
	c := NewCanal(&CanalConfig{
		Host:          "192.168.1.24:3316",
		Username:      "root",
		Password:      "123456",
		Schema:        "game",
		IncludeDBs:    []string{"game(.*)"},
		ExcludeDBs:    []string{"game_(.*)"},
		IncludeTables: []string{},
		ExcludeTables: []string{},
	})
	err := c.Open(t.Context())
	if err != nil {
		t.Fatalf("Open Canal failed: %v", err)
	}
	dbs, err := c.GetDatabases(t.Context())
	if err != nil {
		t.Fatalf("GetDatabases failed: %v", err)
	}

	dbs = hutl.Filter(dbs, func(db string) bool {
		return c.FilterDatabase(db)
	})

	t.Logf("Databases: %v", dbs)
}

func Test_CanalGetTable(t *testing.T) {
	c := NewCanal(&CanalConfig{
		Host:          "192.168.1.24:3316",
		Username:      "root",
		Password:      "123456",
		Schema:        "game",
		IncludeDBs:    []string{"game(.*)"},
		ExcludeDBs:    []string{"game_(.*)"},
		IncludeTables: []string{"(.*)"},
		ExcludeTables: []string{"stats_(.*)"},
	})
	err := c.Open(t.Context())
	if err != nil {
		t.Fatalf("Open Canal failed: %v", err)
	}
	dbs, err := c.GetTables(t.Context(), "game_usa")
	if err != nil {
		t.Fatalf("GetTable failed: %v", err)
	}

	dbs = hutl.Filter(dbs, func(db string) bool {
		return c.FilterTable(db)
	})

	t.Logf("Table: %v", dbs)
}

func Test_CanalGetColumns(t *testing.T) {
	c := NewCanal(&CanalConfig{
		Host:     "192.168.1.24:3316",
		Username: "root",
		Password: "123456",
		Schema:   "game",
	})
	err := c.Open(t.Context())
	if err != nil {
		t.Fatalf("Open Canal failed: %v", err)
	}
	dbs, err := c.GetColumns(t.Context(), "game_usa", "act_info")
	if err != nil {
		t.Fatalf("GetColumns failed: %v", err)
	}

	t.Logf("Columns: %v", dbs)
}
