package db

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"
)

type Sql struct {
	text   strings.Builder
	params []any
}

func NewSql() *Sql {
	return &Sql{
		text:   strings.Builder{},
		params: []any{},
	}
}

func (s *Sql) T(query string) *Sql {
	s.text.WriteString(strings.Trim(strings.Trim(query, " "), "\t"))
	s.text.WriteString(" ")
	return s
}

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
			s.text.WriteString(question)
		}
		s.text.WriteString("? ")
	}
	s.params = append(s.params, args...)
	return s
}

func (s *Sql) ToText() string {
	text := s.text.String()
	for _, param := range s.params {
		text = strings.Replace(text, "?", fmt.Sprint(param), 1)
	}
	return text
}

func (s *Sql) Query(ctx context.Context) (*sql.Rows, error) {
	_ = log.Output(2, fmt.Sprintln(s.ToText()))
	return GET(ctx).Query(s.text.String(), s.params...)
}

func (s *Sql) Exec(ctx context.Context) (sql.Result, error) {
	_ = log.Output(2, fmt.Sprintln(s.ToText()))
	return GET(ctx).Exec(s.text.String(), s.params...)
}
