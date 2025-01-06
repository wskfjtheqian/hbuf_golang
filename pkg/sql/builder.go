package sql

import (
	"context"
	"database/sql"
	"github.com/wskfjtheqian/hbuf_golang/pkg/erro"
	"strings"
)

// NewBuilder 创建一个新的 Builder 实例。
func NewBuilder() *Builder {
	return &Builder{
		text:   strings.Builder{},
		params: []any{},
	}
}

// Builder 是用于构建 SQL 查询的接口。
type Builder struct {
	text     strings.Builder
	params   []any
	cacheKey string
	del      string
}

// T 添加文本
func (s *Builder) T(query string) *Builder {
	s.text.WriteString(s.removeStart(strings.Trim(strings.Trim(query, " "), "\t")))
	s.text.WriteString(" ")
	return s
}

// V 添加值得
func (s *Builder) V(a any) *Builder {
	s.text.WriteString("? ")
	s.params = append(s.params, a)
	return s
}

// P 添加参数
func (s *Builder) P(args ...any) {
	s.params = append(s.params, args...)
}

// L 添加参数列表
func (s *Builder) L(question string, args ...any) *Builder {
	for i, _ := range args {
		if 0 != i {
			s.text.WriteString(s.removeStart(question))
		}
		s.text.WriteString("? ")
	}
	s.params = append(s.params, args...)
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
	return ExplainSQL(text, nil, `'`, s.params...)
}

func (s *Builder) Query(ctx context.Context, scan func(*sql.Rows) (bool, error)) (int64, error) {
	var count int64 = 0
	defer newPrintLog(s, &count).print()

	sql, ok := FromContext(ctx)
	if !ok {
		return 0, erro.NewError("no database connection found in context")
	}
	db, err := sql.GetDB()
	if err != nil {
		return 0, err
	}
	result, err := db.Query(s.text.String(), s.params...)
	if err != nil {
		return 0, err
	}
	defer result.Close()

	isScan := true
	for result.Next() {
		count++
		if isScan {
			isScan, err = scan(result)
			if err != nil {
				return 0, err
			}
		}
	}
	return count, nil
}

func (s *Builder) Exec(ctx context.Context) (int64, int64, error) {
	var count int64 = 0
	defer newPrintLog(s, &count).print()

	result, err := GET(ctx).Exec(s.text.String(), s.params...)
	if err != nil {
		return 0, 0, err
	}
	count, err = result.RowsAffected()
	if err != nil {
		return 0, 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, 0, err
	}
	return count, id, nil
}
