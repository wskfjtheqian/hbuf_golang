package hcdc_test

import (
	"testing"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hutl"
)

func Test_CanalGetDatabase(t *testing.T) {
	c := hcdc.NewCanal(&hcdc.CanalConfig{
		InstanceId:    0,
		Host:          "192.168.1.24:3316",
		Username:      "root",
		Password:      "123456",
		Schema:        "game",
		IncludeDBs:    []string{"game(.*)"},
		ExcludeDBs:    []string{"game_(.*)"},
		IncludeTables: []string{},
		ExcludeTables: []string{},
		ServerID:      nil,
		Charset:       "",
		Flavor:        "",
		LogDir:        "./logs",
	})
	err := c.Open(t.Context())
	if err != nil {
		t.Fatalf("Open Canal failed: %v", err)
	}
	dbs, err := c.GetDatabases(t.Context())
	if err != nil {
		t.Fatalf("GetDatabases failed: %v", err)
	}

	dbs = hutl.Filter(dbs, func(db hcdc.Schema) bool {
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
		LogDir:        "./logs",
	})
	err := c.Open(t.Context())
	if err != nil {
		t.Fatalf("Open Canal failed: %v", err)
	}
	dbs, err := c.GetTables(t.Context(), "game_usa")
	if err != nil {
		t.Fatalf("GetTable failed: %v", err)
	}

	dbs = hutl.Filter(dbs, func(db hcdc.Table) bool {
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
		LogDir:   "./logs",
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
	table := hcdc.Table("stats_agent_reward_report")

	c := hcdc.NewCanal(&hcdc.CanalConfig{
		Host:     "192.168.1.24:3316",
		Username: "root",
		Password: "123456",
		Schema:   "game",
		LogDir:   "./logs",
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
		LogDir:   "./logs",
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
		Password: "",
		Username: "admin",
	})

	d.RegisterWorker(t.Context(), c.GetWorker())

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
				LogDir:        "./logs",
			},
		},
		Doris: &hcdc.DorisConfig{
			Host:     "192.168.1.24:9030",
			LoadURL:  "http://192.168.1.24:8040/",
			Password: "",
			Username: "admin",
		},
	})
	if err != nil {
		herror.PrintStack(t.Context(), err)
	}
	time.Sleep(time.Hour)
}

//func Test_StreamSave(t *testing.T) {
//	c := hcdc.NewCanal(&hcdc.CanalConfig{
//		Host:     "192.168.1.24:3316",
//		Username: "root",
//		Password: "123456",
//		Schema:   "game",
//	})
//	err := c.Open(t.Context())
//	if err != nil {
//		t.Fatalf("Open Canal failed: %v", err)
//
//	}
//	table := hcdc.Table("stats_user_activity_trend") // Changed from string to hcdc.Table
//	info, err := c.GetTableInfo(t.Context(), "game_usa", table)
//	if err != nil {
//		t.Fatalf("GetTableInfo failed: %v", err)
//	}
//
//	d := hcdc.NewDoris(&hcdc.DorisConfig{
//		Host:     "192.168.1.24:9030",
//		LoadURL:  "http://192.168.1.24:8040/",
//		LogDir:   "/Users/dev/2.hbuf/hbuf_golang/pkg/hcdc/logs",
//		Password: "",
//		Username: "admin",
//	})
//
//	w := hcdc.NewWorker("game_usa", table, "/Users/dev/2.hbuf/hbuf_golang/pkg/hcdc/logs")
//
//	_, err = w.ScanFile(t.Context(), func(ctx context.Context, infos []hcdc.ColumnInfo, columns string, reader io.Reader) error {
//		return d.StreamSave(t.Context(), "game_usa", table, info.Columns, columns, reader)
//	})
//	if err != nil {
//		herror.PrintStack(t.Context(), err)
//	}
//}
