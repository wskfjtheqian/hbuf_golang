package mysqlsource

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
)

// Snapshot 持有可重复读视图及其对应的 binlog 游标。
// 持有方应顺序调用 Scan 和 Close。
type Snapshot struct {
	Cursor Cursor
	Tables []Table

	conn *sql.Conn
}

// BeginSnapshot 短暂阻止提交，以建立一致性读视图并绑定 binlog 游标。快照采集完成前，应暂停已选源表的 DDL。
func (s *Source) BeginSnapshot(ctx context.Context) (_ *Snapshot, resultErr error) {
	lockConn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, herror.WrapMsg(err, "obtain source connection for snapshot lock")
	}
	defer lockConn.Close()
	reader, err := s.db.Conn(ctx)
	if err != nil {
		return nil, herror.WrapMsg(err, "obtain source snapshot reader")
	}
	defer func() {
		if resultErr != nil {
			discardConnection(reader)
		}
	}()
	for _, connection := range []*sql.Conn{lockConn, reader} {
		identity, err := s.readSourceIdentity(ctx, connection)
		if err != nil {
			return nil, err
		}
		if identity != s.identity {
			return nil, herror.NewError("snapshot connections resolved to different source servers")
		}
	}
	if s.config.GTIDAutoPosition {
		profile, err := s.gtidProfile(sqlGTIDQuery(ctx, lockConn))
		if err != nil {
			return nil, err
		}
		if profile.readOnly {
			return nil, herror.NewError("GTID auto-position requires a writable source member")
		}
	}
	if _, err := reader.ExecContext(ctx, "SET SESSION TRANSACTION ISOLATION LEVEL REPEATABLE READ"); err != nil {
		return nil, herror.WrapMsg(err, "set source snapshot transaction isolation")
	}
	if _, err := lockConn.ExecContext(ctx, "FLUSH TABLES WITH READ LOCK"); err != nil {
		discardConnection(lockConn)
		return nil, herror.WrapMsg(err, "acquire source global read lock")
	}
	locked := true
	defer func() {
		if locked {
			if err := unlockSnapshot(ctx, lockConn); err != nil {
				resultErr = errors.Join(resultErr, err)
			}
		}
	}()
	if _, err := reader.ExecContext(ctx, "START TRANSACTION WITH CONSISTENT SNAPSHOT, READ ONLY"); err != nil {
		return nil, herror.WrapMsg(err, "start consistent source snapshot")
	}
	cursor, err := snapshotCursor(ctx, lockConn, s.identity, s.config.Flavor)
	if err != nil {
		return nil, err
	}
	if s.config.GTIDAutoPosition && cursor.GTIDSet == "" {
		return nil, herror.NewError("GTID auto-position requires a non-empty snapshot GTID set")
	}
	tables := make([]Table, 0, len(s.config.Tables))
	for _, ref := range s.config.Tables {
		table, err := tableMetadata(ctx, reader, ref, s.config.Flavor)
		if err != nil {
			return nil, err
		}
		// 在事务内访问表会取得元数据锁，使扫描期间的物理结构保持稳定。
		if err := pinTable(ctx, reader, table); err != nil {
			return nil, err
		}
		tables = append(tables, table)
	}

	unlockErr := unlockSnapshot(ctx, lockConn)
	locked = false
	if unlockErr != nil {
		return nil, unlockErr
	}
	hlog.Info(ctx, "source snapshot established file=%s position=%d tables=%d", cursor.File, cursor.Position, len(tables))
	return &Snapshot{Cursor: cursor, Tables: tables, conn: reader}, nil
}

// discardConnection 将连接标记为不可用并关闭，避免带残留状态的连接返回连接池。
func discardConnection(conn *sql.Conn) {
	_ = conn.Raw(func(any) error { return driver.ErrBadConn })
	_ = conn.Close()
}

// unlockSnapshot 在独立的超时范围内释放源端全局读锁，释放失败时丢弃连接。
func unlockSnapshot(ctx context.Context, conn *sql.Conn) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if _, err := conn.ExecContext(cleanup, "UNLOCK TABLES"); err != nil {
		discardConnection(conn)
		return herror.WrapMsg(err, "release source snapshot lock")
	}
	return nil
}

// snapshotCursor 读取快照对应的 binlog 文件、位置和 GTID 集合。
func snapshotCursor(ctx context.Context, conn *sql.Conn, identity, flavor string) (Cursor, error) {
	cursor := Cursor{SourceIdentity: identity, Flavor: flavor}
	query := "SHOW MASTER STATUS"
	if flavor == mysql.MySQLFlavor {
		var err error
		query, err = mySQLStatusQuery(ctx, conn)
		if err != nil {
			return cursor, err
		}
	}
	rows, err := conn.QueryContext(ctx, query)
	if err != nil {
		return cursor, herror.WrapMsg(err, "read source binary log status")
	}
	if err := scanSnapshotCursor(rows, &cursor); err != nil {
		return cursor, err
	}
	if flavor == mysql.MariaDBFlavor {
		if err := conn.QueryRowContext(ctx, "SELECT @@GLOBAL.gtid_binlog_pos").Scan(&cursor.GTIDSet); err != nil {
			return cursor, herror.WrapMsg(err, "read MariaDB binary log GTID set")
		}
	}
	if _, err := mysql.ParseGTIDSet(flavor, cursor.GTIDSet); err != nil {
		return cursor, herror.WrapMsg(err, "parse snapshot GTID set")
	}
	return cursor, nil
}

// mySQLStatusQuery 根据 MySQL 版本选择读取 binlog 状态的语句。
func mySQLStatusQuery(ctx context.Context, conn *sql.Conn) (string, error) {
	var version string
	if err := conn.QueryRowContext(ctx, "SELECT VERSION()").Scan(&version); err != nil {
		return "", herror.WrapMsg(err, "read MySQL version")
	}
	var major, minor int
	if _, err := fmt.Sscanf(version, "%d.%d", &major, &minor); err != nil {
		return "", herror.WrapMsg(err, "parse MySQL version")
	}
	if major > 8 || major == 8 && minor >= 4 {
		return "SHOW BINARY LOG STATUS", nil
	}
	return "SHOW MASTER STATUS", nil
}

// scanSnapshotCursor 按结果列名称提取并校验快照的 binlog 游标。
func scanSnapshotCursor(rows *sql.Rows, cursor *Cursor) error {
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return herror.WrapMsg(err, "read binary log status columns")
	}
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return herror.WrapMsg(err, "read binary log status row")
		}
		return herror.NewError("source returned no binary log status")
	}
	var ignored any
	fields := make([]any, len(columns))
	for i, column := range columns {
		switch strings.ToLower(column) {
		case "file":
			fields[i] = &cursor.File
		case "position":
			fields[i] = &cursor.Position
		case "executed_gtid_set":
			fields[i] = &cursor.GTIDSet
		default:
			fields[i] = &ignored
		}
	}
	if err := rows.Scan(fields...); err != nil {
		return herror.WrapMsg(err, "decode binary log status")
	}
	if cursor.File == "" || cursor.Position < 4 {
		return herror.NewError("source returned an invalid snapshot binary log cursor")
	}
	return nil
}

// pinTable 读取一个源表字段，以便快照事务持有该表的元数据锁。
func pinTable(ctx context.Context, conn *sql.Conn, table Table) error {
	rows, err := conn.QueryContext(ctx, "SELECT "+quoteIdentifier(table.Columns[0].Name)+" FROM "+qualifiedTable(table.Ref)+" LIMIT 1")
	if err != nil {
		return herror.WrapMsg(err, "pin source table "+qualifiedTable(table.Ref))
	}
	if err := rows.Close(); err != nil {
		return herror.WrapMsg(err, "finish source table pin")
	}
	return nil
}

// Scan 在原一致性读视图内按主键顺序逐行读取指定的源表。
func (s *Snapshot) Scan(ctx context.Context, table Table, consume func([]any) error) error {
	if s.conn == nil {
		return herror.NewError("source snapshot is closed")
	}
	var selected *Table
	for i := range s.Tables {
		if s.Tables[i].Ref == table.Ref {
			selected = &s.Tables[i]
			break
		}
	}
	if selected == nil {
		return herror.NewError("scan requested a table outside the source snapshot")
	}
	columns := make([]string, len(selected.Columns))
	for i, column := range selected.Columns {
		columns[i] = quoteIdentifier(column.Name)
	}
	keys := make([]string, len(selected.PrimaryKey))
	for i, index := range selected.PrimaryKey {
		keys[i] = columns[index]
	}
	query := "SELECT " + strings.Join(columns, ",") + " FROM " + qualifiedTable(selected.Ref) + " ORDER BY " + strings.Join(keys, ",")
	rows, err := s.conn.QueryContext(ctx, query)
	if err != nil {
		return herror.WrapMsg(err, "scan source snapshot "+qualifiedTable(selected.Ref))
	}
	defer rows.Close()
	for rows.Next() {
		values := make([]any, len(columns))
		destinations := make([]any, len(columns))
		for i := range values {
			destinations[i] = &values[i]
		}
		if err := rows.Scan(destinations...); err != nil {
			return herror.WrapMsg(err, "decode source snapshot row")
		}
		if err := consume(values); err != nil {
			return err
		}
	}
	if err := rows.Err(); err != nil {
		return herror.WrapMsg(err, "read source snapshot rows")
	}
	return nil
}

// Close 结束源端快照事务，释放读视图、表元数据锁和连接。
func (s *Snapshot) Close() error {
	if s.conn == nil {
		return nil
	}
	conn := s.conn
	s.conn = nil
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := conn.ExecContext(ctx, "ROLLBACK"); err != nil {
		discardConnection(conn)
		return herror.WrapMsg(err, "end source snapshot")
	}
	if err := conn.Close(); err != nil {
		return herror.Wrap(err)
	}
	return nil
}
