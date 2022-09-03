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

func (s *Sql) T(t string) *Sql {
	s.text.WriteString(t)
	return s
}

func (s *Sql) V(a any) *Sql {
	s.text.WriteString(" ? ")
	s.params = append(s.params, a)
	return s
}

func (s *Sql) P(p ...any) {
	s.params = append(s.params, p...)
}

func (s *Sql) L(question string, l ...any) *Sql {
	for i, _ := range l {
		if 0 != i {
			s.text.WriteString(question)
		}
		s.text.WriteString(" ? ")
	}
	s.params = append(s.params, l...)
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
	log.Output(2, fmt.Sprintln(s.ToText()))
	return GET(ctx).Query(s.text.String(), s.params...)
}

func (s *Sql) Exec(ctx context.Context) (sql.Result, error) {
	log.Output(2, fmt.Sprintln(s.ToText()))
	return GET(ctx).Exec(s.text.String(), s.params...)
}