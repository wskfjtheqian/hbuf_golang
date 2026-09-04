package hcdc_test

import (
	"testing"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc"
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
	columns, err := c.GetColumns(t.Context(), "game_usa", "act_info")
	if err != nil {
		t.Fatalf("GetColumns failed: %v", err)
	}

	t.Logf("Columns: %v", columns)
}

func Test_DorisCreateSchema(t *testing.T) {
	d := hcdc.NewDoris(&hcdc.DorisConfig{
		Host:     "192.168.1.24:9030",
		LoadURL:  "192.168.1.24:8040",
		LogDir:   "E:\\develop\\hbuf\\hbuf_golang\\pkg\\hcdc\\logs",
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
	columns, err := c.GetColumns(t.Context(), "game_usa", "user_info")
	if err != nil {
		t.Fatalf("GetColumns failed: %v", err)
	}

	t.Logf("Columns: %v", columns)

	d := hcdc.NewDoris(&hcdc.DorisConfig{
		Host:     "192.168.1.24:9030",
		LoadURL:  "192.168.1.24:8040",
		LogDir:   "E:\\develop\\hbuf\\hbuf_golang\\pkg\\hcdc\\logs",
		Password: "",
		Username: "admin",
	})
	err = d.Open(t.Context())
	if err != nil {
		t.Fatalf("Open Doris failed: %v", err)
	}
	defer d.Close()

	err = d.CreateTable(t.Context(), "game_usa", "user_info", columns, hcdc.PartitionInfo{
		Enable:        false,
		FieldName:     "",
		Type:          "",
		PreCreateDays: 0,
	})
	if err != nil {
		t.Fatalf("CreateTable failed: %v", err)
	}
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

	columns, err := c.GetColumns(t.Context(), "game_usa", "user_info")
	if err != nil {
		t.Fatalf("GetColumns failed: %v", err)
	}

	t.Logf("Columns: %v", columns)

	d := hcdc.NewDoris(&hcdc.DorisConfig{
		Host:     "192.168.1.24:9030",
		LoadURL:  "http://192.168.1.24:8040/",
		LogDir:   "E:\\develop\\hbuf\\hbuf_golang\\pkg\\hcdc\\logs",
		Password: "",
		Username: "admin",
	})

	d.RegisterWorker(t.Context(), "game_usa", "user_info")

	err = d.Open(t.Context())
	if err != nil {
		t.Fatalf("Open Doris failed: %v", err)
	}
	defer d.Close()

	err = c.ReadData(t.Context(), "game_usa", "user_info", columns, "0", "2360005")
	if err != nil {
		t.Fatalf("CreateTable failed: %v", err)
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
				IncludeTables: []string{"user_info"},
			},
		},
		Doris: &hcdc.DorisConfig{
			Host:     "192.168.1.24:9030",
			LoadURL:  "http://192.168.1.24:8040/",
			LogDir:   "E:\\develop\\hbuf\\hbuf_golang\\pkg\\hcdc\\logs",
			Password: "",
			Username: "admin",
		},
	})
	if err != nil {
		t.Fatalf("SetConfig failed: %v", err)
	}
	time.Sleep(time.Hour)
}
