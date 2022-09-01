package db

import "testing"

func TestSql_ToText(t *testing.T) {
	s := NewSql()
	s.T("SELECT name, age, id FROM class WHERE age < ").V(23).T("AND id IN(").L(",", 1, 2, 3).T(") LIMIT").V(0).T(",").V(20)
	println(s.ToText())
}
