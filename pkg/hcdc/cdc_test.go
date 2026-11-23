package hcdc_test

import (
	"os"
	"testing"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hutl"
)

const (
	defaultMysqlHost = "127.0.0.1:3316"
	defaultDorisHost = "127.0.0.1:9130"
	defaultLoadURL   = "http://127.0.0.1:8140/"
	defaultMysqlUser = "root"
	defaultMysqlPass = "123456"
	defaultDorisUser = "admin"
	defaultDorisPass = ""
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func Test_CanalGetDatabase(t *testing.T) {
	c := hcdc.NewCanal(&hcdc.CanalConfig{
		InstanceId:    0,
		Host:          envOr("CDC_MYSQL_HOST", defaultMysqlHost),
		Username:      envOr("CDC_MYSQL_USER", defaultMysqlUser),
		Password:      envOr("CDC_MYSQL_PASS", defaultMysqlPass),
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
		Host:          envOr("CDC_MYSQL_HOST", defaultMysqlHost),
		Username:      envOr("CDC_MYSQL_USER", defaultMysqlUser),
		Password:      envOr("CDC_MYSQL_PASS", defaultMysqlPass),
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
		Host:     envOr("CDC_MYSQL_HOST", defaultMysqlHost),
		Username: envOr("CDC_MYSQL_USER", defaultMysqlUser),
		Password: envOr("CDC_MYSQL_PASS", defaultMysqlPass),
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
		Host:     envOr("CDC_DORIS_HOST", defaultDorisHost),
		LoadURL:  envOr("CDC_DORIS_LOAD_URL", defaultLoadURL),
		Password: envOr("CDC_DORIS_PASS", defaultDorisPass),
		Username: envOr("CDC_DORIS_USER", defaultDorisUser),
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
		Host:     envOr("CDC_MYSQL_HOST", defaultMysqlHost),
		Username: envOr("CDC_MYSQL_USER", defaultMysqlUser),
		Password: envOr("CDC_MYSQL_PASS", defaultMysqlPass),
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
		Host:     envOr("CDC_DORIS_HOST", defaultDorisHost),
		LoadURL:  envOr("CDC_DORIS_LOAD_URL", defaultLoadURL),
		Password: envOr("CDC_DORIS_PASS", defaultDorisPass),
		Username: envOr("CDC_DORIS_USER", defaultDorisUser),
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
		Host:     envOr("CDC_MYSQL_HOST", defaultMysqlHost),
		Username: envOr("CDC_MYSQL_USER", defaultMysqlUser),
		Password: envOr("CDC_MYSQL_PASS", defaultMysqlPass),
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
		Host:     envOr("CDC_DORIS_HOST", defaultDorisHost),
		LoadURL:  envOr("CDC_DORIS_LOAD_URL", defaultLoadURL),
		Password: envOr("CDC_DORIS_PASS", defaultDorisPass),
		Username: envOr("CDC_DORIS_USER", defaultDorisUser),
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
				Host:          envOr("CDC_MYSQL_HOST", defaultMysqlHost),
				Username:      envOr("CDC_MYSQL_USER", defaultMysqlUser),
				Password:      envOr("CDC_MYSQL_PASS", defaultMysqlPass),
				Schema:        "game",
				IncludeDBs:    []string{"game_usa"},
				IncludeTables: []string{"(.*)"},
				LogDir:        "./logs",
			},
		},
		Doris: &hcdc.DorisConfig{
			Host:     envOr("CDC_DORIS_HOST", defaultDorisHost),
			LoadURL:  envOr("CDC_DORIS_LOAD_URL", defaultLoadURL),
			Password: envOr("CDC_DORIS_PASS", defaultDorisPass),
			Username: envOr("CDC_DORIS_USER", defaultDorisUser),
		},
	})
	if err != nil {
		herror.PrintStack(t.Context(), err)
	}
	//time.Sleep(time.Hour)
}
