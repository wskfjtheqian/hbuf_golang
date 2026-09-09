package hcdc

import (
	"context"
	"strings"
	"sync/atomic"
)

type Schema string
type Table string
type Column string
type SchemaTable string
type RawBytes []byte
type Action int

const (
	Insert Action = iota
	Update
	Delete
	Create
)

type ColumnInfo struct {
	Name     Column
	Type     string
	Args     string
	Comment  string
	KeyIndex int
	IsNull   string
	Default  *string
	Action   string
	OldName  Column
}

func (i ColumnInfo) String() string {
	return string(i.Name) + ":" + i.Type
}

type TableInfo struct {
	Schema         Schema
	Table          Table
	Columns        []ColumnInfo
	Index          map[Column]int
	Keys           []Column
	PartitionField Column // 分区字段名
	PartitionType  string // 分区类型，目前主流为 "RANGE"
	Action         string
	IsCreate       bool
	IsAlter        bool
	IsDrop         bool
}

type Config struct {
	Canals []CanalConfig `yaml:"canals"` // Canal 配置
	Doris  *DorisConfig  `yaml:"doris"`  // Doris 配置
}

func (c *Config) Validate(ctx context.Context) bool {
	var valid bool = true
	return valid
}

func (c *Config) Equal(other *Config) bool {
	if c == nil && other == nil {
		return true
	}
	if c == nil || other == nil {
		return false
	}
	if len(c.Canals) != len(other.Canals) {
		return false
	}
	for i := range c.Canals {
		if !c.Canals[i].Equal(&other.Canals[i]) {
			return false
		}
	}
	return c.Doris.Equal(other.Doris)

}

type HCDC struct {
	canal atomic.Pointer[[]*Canal]
	doris atomic.Pointer[Doris]
	cfg   *Config
}

func NewHCDC() *HCDC {
	return &HCDC{}
}

func (h *HCDC) SetConfig(ctx context.Context, cfg *Config) error {
	if h.cfg != nil && h.cfg.Equal(cfg) {
		return nil
	}

	if cfg == nil {
		if h.doris.Load() != nil {
			_ = h.doris.Load().Close()
		}
		h.doris.Store(nil)

		canals := h.canal.Load()
		if canals != nil {
			for _, canal := range *canals {
				canal.Close()
			}
		}
		h.canal.Store(nil)
		return nil
	}

	doris := NewDoris(cfg.Doris)
	err := doris.Open(ctx)
	if err != nil {
		return err
	}
	h.doris.Store(doris)

	canals := make([]*Canal, len(cfg.Canals))
	for i, item := range cfg.Canals {
		canals[i] = NewCanal(&item)
		h.setCanalCall(canals[i])
		err := canals[i].Open(ctx)
		if err != nil {
			for j := 0; j < i; j++ {
				canals[j].Close()
			}
			_ = doris.Close()
			return err
		}
	}
	h.canal.Store(&canals)
	return nil
}

func (h *HCDC) setCanalCall(canal *Canal) {
	canal.SetOnData(func(ctx context.Context, schema Schema, table Table, action Action, columns []ColumnInfo, values [][]RawBytes) error {
		doris := h.doris.Load()
		if doris == nil {
			return nil
		}
		return doris.AddData(ctx, schema, table, action, columns, values)
	})

	canal.setOnCreateSchema(func(ctx context.Context, schema Schema) error {
		doris := h.doris.Load()
		if doris == nil {
			return nil
		}
		return doris.CreateSchema(ctx, schema)
	})

	canal.setOnCreateTable(func(ctx context.Context, infos []TableInfo) error {
		doris := h.doris.Load()
		if doris == nil {
			return nil
		}
		doris.ChangeTables(ctx, infos)
		for _, tableInfo := range infos {
			doris.RegisterWorker(ctx, tableInfo.Schema, tableInfo.Table)
		}

		return nil
	})
}

func GetColumnType(col *ColumnInfo) string {
	if col.Type == "binary" || col.Type == "varbinary" || col.Type == "blob" || col.Type == "longblob" || col.Type == "mediumblob" {
		if strings.Contains(col.Comment, "CustomType=Bitmap64") {
			return "bitmap64"
		} else if strings.Contains(col.Comment, "CustomType=Bitmap32") {
			return "bitmap32"
		}

	}
	return col.Type
}
