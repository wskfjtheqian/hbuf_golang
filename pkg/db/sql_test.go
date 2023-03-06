package db

import (
	"context"
	"database/sql"
	"testing"
	"time"
)

func TestSql_ToText(t *testing.T) {
	s := NewSql()
	s.T("SELECT name, age, id FROM class WHERE age < ").V(23).T("AND name = ").V(time.Now()).T("AND id IN(").L(",", 1, 2, 3).T(") LIMIT").V(0).T(",").V(20)
	println(s.ToText())
}

func TestSql_Query(t *testing.T) {
	s := NewSql()

	_, err := s.Query(context.TODO(), func(rows *sql.Rows) (bool, error) {
		err := rows.Scan()
		if nil != err {

		}
		return false, err
	})
	if err != nil {
		return
	}
}

func TestSql_Exec(t *testing.T) {
	s := NewSql()

	_, err := s.Exec(context.TODO())
	if err != nil {
		return
	}
}
