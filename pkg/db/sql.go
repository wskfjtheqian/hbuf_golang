package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	utl "github.com/wskfjtheqian/hbuf_golang/pkg/utils"
	"log"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
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

type Sql struct {
	text     strings.Builder
	params   []any
	cacheKey string
	del      string
}

func NewSql() *Sql {
	return &Sql{
		text:   strings.Builder{},
		params: []any{},
	}
}

// T 添加文本
func (s *Sql) T(query string) *Sql {
	s.text.WriteString(s.removeStart(strings.Trim(strings.Trim(query, " "), "\t")))
	s.text.WriteString(" ")
	return s
}

// V 添加值得
func (s *Sql) V(a any) *Sql {
	s.text.WriteString("? ")
	s.params = append(s.params, a)
	return s
}

func (s *Sql) P(args ...any) {
	s.params = append(s.params, args...)
}

func (s *Sql) L(question string, args ...any) *Sql {
	for i, _ := range args {
		if 0 != i {
			s.text.WriteString(s.removeStart(question))
		}
		s.text.WriteString("? ")
	}
	s.params = append(s.params, args...)
	return s
}

func (s *Sql) removeStart(question string) string {
	if len(s.del) > 0 {
		if 0 == strings.Index(question, s.del) {
			question = question[len(s.del):]
		}
		s.del = ""
	}
	return question
}

func (s *Sql) Del(text string) {
	s.del = text
}

func (s *Sql) ToText() string {
	text := s.text.String()
	return ExplainSQL(text, nil, `'`, s.params...)
}

func (s *Sql) Query(ctx context.Context, scan func(*sql.Rows) (bool, error)) (int64, error) {
	var now = time.Now().UnixMilli()
	result, err := GET(ctx).Query(s.text.String(), s.params...)
	if err != nil {
		printLog(now, 0, s.ToText())
		return 0, err
	}
	defer result.Close()
	var count int64 = 0
	isScan := true
	for result.Next() {
		count++
		if isScan {
			isScan, err = scan(result)
			if err != nil {
				printLog(now, 0, s.ToText())
				return 0, err
			}
		}
	}
	printLog(now, count, s.ToText())
	return count, nil
}

func (s *Sql) Exec(ctx context.Context) (int64, int64, error) {
	var now = time.Now().UnixMilli()
	result, err := GET(ctx).Exec(s.text.String(), s.params...)
	if err != nil {
		printLog(now, 0, s.ToText())
		return 0, 0, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		printLog(now, 0, s.ToText())
		return 0, 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		printLog(now, 0, s.ToText())
		return 0, 0, err
	}
	printLog(now, count, s.ToText())
	return count, id, nil
}

func printLog(now, count int64, sql string) {
	now = time.Now().UnixMilli() - now
	t := "[" + strconv.FormatFloat(float64(now)/1000, 'g', 3, 64) + "s]"
	if 200 > now {
		t = utl.Yellow(t)
	} else {
		t = utl.Red(t)
	}
	_ = log.Output(3, fmt.Sprintln(t, utl.Blue("[Rows:"+strconv.FormatInt(count, 10)+"] "), utl.Green(sql)))
}

func ExplainSQL(sql string, numericPlaceholder *regexp.Regexp, escaper string, avars ...interface{}) string {
	var (
		convertParams func(interface{}, int)
		vars          = make([]string, len(avars))
	)

	convertParams = func(v interface{}, idx int) {
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

	if numericPlaceholder == nil {
		var idx int
		var newSQL strings.Builder

		for _, v := range []byte(sql) {
			if v == '?' {
				if len(vars) > idx {
					newSQL.WriteString(vars[idx])
					idx++
					continue
				}
			}
			newSQL.WriteByte(v)
		}

		sql = newSQL.String()
	} else {
		sql = numericPlaceholder.ReplaceAllString(sql, "$$$1$$")
		for idx, v := range vars {
			sql = strings.Replace(sql, "$"+strconv.Itoa(idx+1)+"$", v, 1)
		}
	}

	return sql
}
