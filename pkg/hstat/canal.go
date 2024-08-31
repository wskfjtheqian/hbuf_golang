package hstat

import (
	"github.com/go-mysql-org/go-mysql/canal"
	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/wskfjtheqian/hbuf_golang/pkg/erro"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"reflect"
	"strings"
)

type DbScan interface {
	DbScan() (string, []any)

	DbName() string
}

type pos struct {
	src  int
	desc int
}

func findKey(keys []string, key string) int {
	for i, item := range keys {
		if item == key {
			return i
		}
	}
	return -1
}

func NewCanal(cfg *canal.Config) (*Canal, error) {
	cfg.ParseTime = true
	c, err := canal.NewCanal(cfg)
	if err != nil {
		return nil, erro.Wrap(err)
	}
	ret := &Canal{
		config:  cfg,
		canal:   c,
		handler: make(map[string]func(e *canal.RowsEvent) error),
	}
	c.SetEventHandler(ret)
	return ret, nil
}

type Canal struct {
	canal.DummyEventHandler

	config *canal.Config

	canal *canal.Canal

	pos mysql.Position

	handler map[string]func(e *canal.RowsEvent) error
}

func SetTableHandler[T DbScan](c *Canal, schema string, f func(e *canal.RowsEvent, rows []T) error) {
	var ret any = new(T)
	dpv := reflect.ValueOf(ret)
	dv := reflect.Indirect(dpv)
	dv.Set(reflect.New(dv.Type().Elem()))
	ret = dv.Interface()

	keyStr, _ := (ret).(DbScan).DbScan()
	keys := strings.Split(keyStr, ",")
	for i, key := range keys {
		keys[i] = strings.Trim(key, " ")
	}

	c.handler[schema+"."+(ret).(DbScan).DbName()] = func(e *canal.RowsEvent) error {
		ps := make([]pos, 0)
		for src, item := range e.Table.Columns {
			desc := findKey(keys, item.Name)
			if desc != -1 {
				ps = append(ps, pos{
					src:  src,
					desc: desc,
				})
			}
		}

		list := make([]T, len(e.Rows))
		for i, row := range e.Rows {
			list[i] = reflect.New(dv.Type().Elem()).Interface().(T)
			_, vals := list[i].DbScan()
			for _, p := range ps {
				err := convertAssignRows(vals[p.desc], row[p.src])
				if err != nil {
					return err
				}
			}
		}
		return f(e, list)
	}
}

func (c *Canal) OnRow(e *canal.RowsEvent) error {
	handler, ok := c.handler[e.Table.Schema+"."+e.Table.Name]
	if !ok {
		return nil
	}
	return handler(e)
}

func (c *Canal) Run() {
	go func() {
		err := c.canal.RunFrom(c.pos)
		if err != nil {
			hlog.Exit(err)
		}
	}()
}
