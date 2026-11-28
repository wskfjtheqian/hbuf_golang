package mysqlsource

import (
	"context"

	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
)

// Identity 返回当前 SQL 连接和 Binlog 订阅共同校验的源成员标识。
func (s *Source) Identity() string { return s.identity }

// DiscoverTables 从当前源成员查找给定库中可见的基础表。
func (s *Source) DiscoverTables(ctx context.Context, schema string) ([]string, error) {
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, herror.WrapMsg(err, "obtain source table discovery connection")
	}
	defer conn.Close()
	identity, err := s.readSourceIdentity(ctx, conn)
	if err != nil {
		return nil, err
	}
	if identity != s.identity {
		return nil, herror.NewError("table discovery resolved to another source member")
	}
	rows, err := conn.QueryContext(ctx, `SELECT TABLE_NAME FROM information_schema.TABLES
		WHERE TABLE_SCHEMA = ? AND BINARY TABLE_SCHEMA = ? AND TABLE_TYPE = 'BASE TABLE' ORDER BY TABLE_NAME`, schema, schema)
	if err != nil {
		return nil, herror.WrapMsg(err, "read source table discovery metadata")
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, herror.Wrap(err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, herror.Wrap(err)
	}
	return names, nil
}
