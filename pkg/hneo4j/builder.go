package hneo4j

import (
	"context"
	"database/sql/driver"
	"fmt"
	"reflect"
	"unicode"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hutl"

	"strconv"
	"strings"
	"time"
)

const (
	tmFmtWithMS = "2006-01-02 15:04:05.999"
	tmFmtZero   = "0000-00-00 00:00:00"
	nullStr     = "NULL"
)

var convertibleTypes = []reflect.Type{reflect.TypeOf(time.Time{}), reflect.TypeOf(false), reflect.TypeOf([]byte{})}

func isPrintable(s string) bool {
	for _, r := range s {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

func ToString(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case int:
		return strconv.FormatInt(int64(v), 10)
	case int8:
		return strconv.FormatInt(int64(v), 10)
	case int16:
		return strconv.FormatInt(int64(v), 10)
	case int32:
		return strconv.FormatInt(int64(v), 10)
	case int64:
		return strconv.FormatInt(v, 10)
	case uint:
		return strconv.FormatUint(uint64(v), 10)
	case uint8:
		return strconv.FormatUint(uint64(v), 10)
	case uint16:
		return strconv.FormatUint(uint64(v), 10)
	case uint32:
		return strconv.FormatUint(uint64(v), 10)
	case uint64:
		return strconv.FormatUint(v, 10)
	}
	return ""
}

// NewBuilder 创建一个新的 Builder 实例。
func NewBuilder() *Builder {
	return &Builder{
		text:   strings.Builder{},
		params: map[string]any{},
	}
}

// Builder 是用于构建 SQL 查询的接口。
type Builder struct {
	text     strings.Builder
	params   map[string]any
	cacheKey string
	del      string
	index    uint64
}

// T 添加文本
func (s *Builder) T(query string) *Builder {
	s.text.WriteString(s.removeStart(strings.Trim(strings.Trim(query, " "), "\t")))
	s.text.WriteString(" ")
	return s
}

// V 添加值得
func (s *Builder) V(a any) *Builder {
	index := "p" + strconv.FormatUint(s.index, 10)
	s.text.WriteString("$" + index)
	s.params[index] = a
	s.index++
	return s
}

// P 添加参数
func (s *Builder) P(args ...any) {
	for _, arg := range args {
		index := "p" + strconv.FormatUint(s.index, 10)
		s.text.WriteString("$" + index)
		s.params[index] = arg
		s.index++
	}
}

// L 添加参数列表
func (s *Builder) L(question string, args ...any) *Builder {
	for i, val := range args {
		if 0 != i {
			s.text.WriteString(s.removeStart(question))
		}
		index := "p" + strconv.FormatUint(s.index, 10)
		s.text.WriteString("$" + index)
		s.params[index] = val
		s.index++
	}
	return s
}

func (s *Builder) removeStart(question string) string {
	if len(s.del) > 0 {
		if 0 == strings.Index(question, s.del) {
			question = question[len(s.del):]
		}
		s.del = ""
	}
	return question
}

func (s *Builder) Del(text string) {
	s.del = text
}

func (s *Builder) ToText() string {
	text := s.text.String()
	return ExplainSQL(text, `'`, s.params)
}

func (s *Builder) Query(ctx context.Context, scan func(record *neo4j.Record) (bool, error)) (int64, error) {
	var count int64 = 0
	defer newPrintLog(s, &count).print()

	c, ok := FromContext(ctx)
	if !ok {
		return 0, herror.NewError("no Neo4j connection found in context")
	}
	session := c.Get(ctx)
	defer session.Close(ctx)

	records, err := session.Run(ctx, s.text.String(), s.params)
	if err != nil {
		return 0, herror.Wrap(err)
	}

	for records.Next(ctx) {
		count++
		if scan != nil {
			ok, err := scan(records.Record())
			if err != nil {
				return 0, err
			}
			if !ok {
				return 0, nil
			}
		}
	}
	return count, nil
}

func ExplainSQL(sql string, escaper string, avars map[string]any) string {
	var (
		convertParams func(interface{}, string)
		vars          = make(map[string]string, len(avars))
	)

	convertParams = func(v interface{}, idx string) {
		switch v := v.(type) {
		case bool:
			vars[idx] = strconv.FormatBool(v)
		case time.Time:
			if v.IsZero() {
				vars[idx] = escaper + tmFmtZero + escaper
			} else {
				vars[idx] = escaper + v.Format(tmFmtWithMS) + escaper
			}
		case *time.Time:
			if v != nil {
				if v.IsZero() {
					vars[idx] = escaper + tmFmtZero + escaper
				} else {
					vars[idx] = escaper + v.Format(tmFmtWithMS) + escaper
				}
			} else {
				vars[idx] = nullStr
			}
		case driver.Valuer:
			reflectValue := reflect.ValueOf(v)
			if v != nil && reflectValue.IsValid() && ((reflectValue.Kind() == reflect.Ptr && !reflectValue.IsNil()) || reflectValue.Kind() != reflect.Ptr) {
				r, _ := v.Value()
				convertParams(r, idx)
			} else {
				vars[idx] = nullStr
			}
		case fmt.Stringer:
			reflectValue := reflect.ValueOf(v)
			switch reflectValue.Kind() {
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
				vars[idx] = fmt.Sprintf("%d", reflectValue.Interface())
			case reflect.Float32, reflect.Float64:
				vars[idx] = fmt.Sprintf("%.6f", reflectValue.Interface())
			case reflect.Bool:
				vars[idx] = fmt.Sprintf("%t", reflectValue.Interface())
			case reflect.String:
				vars[idx] = escaper + strings.ReplaceAll(fmt.Sprintf("%v", v), escaper, "\\"+escaper) + escaper
			default:
				if v != nil && reflectValue.IsValid() && ((reflectValue.Kind() == reflect.Ptr && !reflectValue.IsNil()) || reflectValue.Kind() != reflect.Ptr) {
					vars[idx] = escaper + strings.ReplaceAll(fmt.Sprintf("%v", v), escaper, "\\"+escaper) + escaper
				} else {
					vars[idx] = nullStr
				}
			}
		case []byte:
			if s := string(v); isPrintable(s) {
				vars[idx] = escaper + strings.ReplaceAll(s, escaper, "\\"+escaper) + escaper
			} else {
				vars[idx] = escaper + "<binary>" + escaper
			}
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			vars[idx] = ToString(v)
		case float64, float32:
			vars[idx] = fmt.Sprintf("%.6f", v)
		case string:
			vars[idx] = escaper + strings.ReplaceAll(v, escaper, "\\"+escaper) + escaper
		default:
			rv := reflect.ValueOf(v)
			if v == nil || !rv.IsValid() || rv.Kind() == reflect.Ptr && rv.IsNil() {
				vars[idx] = nullStr
			} else if valuer, ok := v.(driver.Valuer); ok {
				v, _ = valuer.Value()
				convertParams(v, idx)
			} else if rv.Kind() == reflect.Ptr && !rv.IsZero() {
				convertParams(reflect.Indirect(rv).Interface(), idx)
			} else {
				for _, t := range convertibleTypes {
					if rv.Type().ConvertibleTo(t) {
						convertParams(rv.Convert(t).Interface(), idx)
						return
					}
				}
				vars[idx] = escaper + strings.ReplaceAll(fmt.Sprint(v), escaper, "\\"+escaper) + escaper
			}
		}
	}

	for idx, v := range avars {
		convertParams(v, idx)
	}

	for key, v := range vars {
		sql = strings.Replace(sql, "$"+key, v, 1)
	}
	return sql
}

//=======================================================================================================================

type printLog struct {
	now     time.Time
	count   *int64
	builder *Builder
}

func (p *printLog) print() {
	if !PrintSQL {
		return
	}
	dur := time.Since(p.now) / time.Millisecond

	text := strings.Builder{}
	t := "[" + strconv.FormatFloat(float64(dur), 'f', 3, 64) + "ms]"
	if 200 > dur {
		text.WriteString(hutl.Yellow(t))
	} else {
		text.WriteString(hutl.Red(t))
	}
	text.WriteString(hutl.Blue("[Rows:" + strconv.FormatInt(*p.count, 10) + "] "))
	text.WriteString(hutl.Green(p.builder.ToText()))

	_ = hlog.Output(3, LogSQL, text.String())
}

func newPrintLog(builder *Builder, count *int64) *printLog {
	ret := &printLog{
		builder: builder,
		count:   count,
	}
	if PrintSQL {
		ret.now = time.Now()
	}
	return ret
}

var LogSQL = hlog.INFO + 200
var PrintSQL = true

func init() {
	hlog.SetLevelName(LogSQL, "Neo4j")
}
