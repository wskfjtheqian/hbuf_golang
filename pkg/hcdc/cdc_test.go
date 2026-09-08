package hcdc_test

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hutl"
)

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
	c := hcdc.NewCanal(&hcdc.CanalConfig{
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
	c := hcdc.NewCanal(&hcdc.CanalConfig{
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
	c := hcdc.NewCanal(&hcdc.CanalConfig{
		Host:     "192.168.1.24:3316",
		Username: "root",
		Password: "123456",
		Schema:   "game",
	})
	err := c.Open(t.Context())
	if err != nil {
		t.Fatalf("Open Canal failed: %v", err)
	}
	columns, err := c.GetTableInfo(t.Context(), "game_usa", "game_record")
	if err != nil {
		t.Fatalf("GetTableInfo failed: %v", err)
	}

	t.Logf("Columns: %v", columns)
}

func Test_DorisCreateSchema(t *testing.T) {
	d := hcdc.NewDoris(&hcdc.DorisConfig{
		Host:     "192.168.1.24:9030",
		LoadURL:  "192.168.1.24:8040",
		LogDir:   "/Users/dev/2.hbuf/hbuf_golang/pkg/hcdc/logs",
		Password: "",
		Username: "admin",
	})
	err := d.Open(t.Context())
	if err != nil {
		t.Fatalf("Open Doris failed: %v", err)
	}
	defer d.Close()
	err = d.CreateSchema(t.Context(), "game_usa")
	if err != nil {
		t.Fatalf("CreateSchema failed: %v", err)
	}
}

func Test_DorisCreateTable(t *testing.T) {
	table := hcdc.Table("act_info")

	c := hcdc.NewCanal(&hcdc.CanalConfig{
		Host:     "192.168.1.24:3316",
		Username: "root",
		Password: "123456",
		Schema:   "game",
	})
	err := c.Open(t.Context())
	if err != nil {
		t.Fatalf("Open Canal failed: %v", err)
	}
	info, err := c.GetTableInfo(t.Context(), "game_usa", table)
	if err != nil {
		t.Fatalf("GetTableInfo failed: %v", err)
	}

	t.Logf("Columns: %v", info)

	d := hcdc.NewDoris(&hcdc.DorisConfig{
		Host:     "192.168.1.24:9030",
		LoadURL:  "192.168.1.24:8040",
		LogDir:   "/Users/dev/2.hbuf/hbuf_golang/pkg/hcdc/logs",
		Password: "",
		Username: "admin",
	})
	err = d.Open(t.Context())
	if err != nil {
		t.Fatalf("Open Doris failed: %v", err)
	}
	defer d.Close()

	d.ChangeTables(t.Context(), []hcdc.TableInfo{*info})
	time.Sleep(30 * time.Second)
}

func Test_DorisCopyTable(t *testing.T) {
	c := hcdc.NewCanal(&hcdc.CanalConfig{
		Host:     "192.168.1.24:3316",
		Username: "root",
		Password: "123456",
		Schema:   "game",
	})
	err := c.Open(t.Context())
	if err != nil {
		t.Fatalf("Open Canal failed: %v", err)
	}
	defer c.Close()

	table := hcdc.Table("stats_agent_reward_report")
	info, err := c.GetTableInfo(t.Context(), "game_usa", table)
	if err != nil {
		t.Fatalf("GetTableInfo failed: %v", err)
	}

	t.Logf("Columns: %v", info)

	d := hcdc.NewDoris(&hcdc.DorisConfig{
		Host:     "192.168.1.24:9030",
		LoadURL:  "http://192.168.1.24:8040/",
		LogDir:   "./logs",
		Password: "",
		Username: "admin",
	})

	d.RegisterWorker(t.Context(), "game_usa", table)
	c.SetOnData(func(ctx context.Context, schema hcdc.Schema, table hcdc.Table, action hcdc.Action, columns []hcdc.ColumnInfo, values [][]hcdc.RawBytes) error {
		return d.AddData(ctx, schema, table, action, columns, values)
	})

	err = d.Open(t.Context())
	if err != nil {
		t.Fatalf("Open Doris failed: %v", err)
	}
	defer d.Close()

	err = c.ReadData(t.Context(), "game_usa", table, *info, "0", "2360005")
	if err != nil {
		t.Fatalf("createTable failed: %v", err)
	}

	time.Sleep(30 * time.Second)
}
func Test_HCDC(t *testing.T) {
	cdc := hcdc.NewHCDC()
	err := cdc.SetConfig(t.Context(), &hcdc.Config{
		Canals: []hcdc.CanalConfig{
			{
				Host:          "192.168.1.24:3316",
				Username:      "root",
				Password:      "123456",
				Schema:        "game",
				IncludeDBs:    []string{"game_usa"},
				IncludeTables: []string{"(.*)"},
			},
		},
		Doris: &hcdc.DorisConfig{
			Host:     "192.168.1.24:9030",
			LoadURL:  "http://192.168.1.24:8040/",
			LogDir:   "./logs",
			Password: "",
			Username: "admin",
		},
	})
	if err != nil {
		herror.PrintStack(t.Context(), err)
	}
	time.Sleep(time.Hour)
}

func Test_StreamSave(t *testing.T) {
	c := hcdc.NewCanal(&hcdc.CanalConfig{
		Host:     "192.168.1.24:3316",
		Username: "root",
		Password: "123456",
		Schema:   "game",
	})
	err := c.Open(t.Context())
	if err != nil {
		t.Fatalf("Open Canal failed: %v", err)

	}
	table := hcdc.Table("stats_user_activity_trend") // Changed from string to hcdc.Table
	info, err := c.GetTableInfo(t.Context(), "game_usa", table)
	if err != nil {
		t.Fatalf("GetTableInfo failed: %v", err)
	}

	d := hcdc.NewDoris(&hcdc.DorisConfig{
		Host:     "192.168.1.24:9030",
		LoadURL:  "http://192.168.1.24:8040/",
		LogDir:   "/Users/dev/2.hbuf/hbuf_golang/pkg/hcdc/logs",
		Password: "",
		Username: "admin",
	})

	w := hcdc.NewWorker("game_usa", table, "/Users/dev/2.hbuf/hbuf_golang/pkg/hcdc/logs")

	err = w.ScanFile(t.Context(), func(ctx context.Context, infos []hcdc.ColumnInfo, columns string, reader io.Reader) error {
		return d.StreamSave(t.Context(), "game_usa", table, info.Columns, columns, reader)
	})
	if err != nil {
		herror.PrintStack(t.Context(), err)
	}
}
