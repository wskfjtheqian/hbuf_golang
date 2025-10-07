package hneo4j

import (
	"context"
	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"regexp"
	"strconv"
	"strings"
)

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
	return ExplainSQL(text, nil, `'`, s.params)
}

func (s *Builder) Query(ctx context.Context, scan func(record *neo4j.Record) (bool, error)) (int64, error) {
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

	var count int64 = 0
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

func ExplainSQL(sql string, numericPlaceholder *regexp.Regexp, escaper string, avars map[string]any) string {
	return ""
}
