package mysqlsource

import (
	"context"
	"database/sql"
	"strings"

	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
)

// validateResume 校验恢复游标的源身份和日志位置，并在启用自动定位时验证 GTID 历史。
func (s *Source) validateResume(ctx context.Context, cursor Cursor) (mysql.GTIDSet, error) {
	if cursor.Flavor != s.config.Flavor || cursor.File == "" || cursor.Position < 4 {
		return nil, herror.NewError("source recovery cursor has an invalid flavor or physical position")
	}
	prefix := s.config.SourceIdentity + "/"
	if (!s.config.GTIDAutoPosition && cursor.SourceIdentity != s.identity) ||
		(s.config.GTIDAutoPosition && (!strings.HasPrefix(cursor.SourceIdentity, prefix) || cursor.SourceIdentity == prefix)) {
		return nil, herror.NewError("source recovery cursor belongs to a different source")
	}
	committed, err := mysql.ParseGTIDSet(cursor.Flavor, cursor.GTIDSet)
	if err != nil {
		return nil, herror.WrapMsg(err, "parse recovery GTID set")
	}
	identity, err := s.preflight(ctx)
	if err != nil {
		return nil, err
	}
	if identity != s.identity {
		return nil, herror.NewError("source server identity changed before starting replication")
	}
	if !s.config.GTIDAutoPosition {
		return committed, nil
	}
	if committed.IsEmpty() {
		return nil, herror.NewError("GTID auto-position requires a non-empty durable GTID set")
	}
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, herror.WrapMsg(err, "obtain source connection for GTID validation")
	}
	defer conn.Close()
	identity, err = s.readSourceIdentity(ctx, conn)
	if err != nil {
		return nil, err
	}
	if identity != s.identity {
		return nil, herror.NewError("GTID validation connection resolved to a different source member")
	}
	if _, err := s.verifyGTIDResume(sqlGTIDQuery(ctx, conn), committed); err != nil {
		return nil, err
	}
	return committed, nil
}

type gtidQuery func(string, ...any) ([]string, error)

// sqlGTIDQuery 将 SQL 连接包装成返回首行字符串值的 GTID 查询函数。
func sqlGTIDQuery(ctx context.Context, conn *sql.Conn) gtidQuery {
	return func(query string, args ...any) ([]string, error) {
		rows, err := conn.QueryContext(ctx, query, args...)
		if err != nil {
			return nil, herror.Wrap(err)
		}
		defer rows.Close()
		columns, err := rows.Columns()
		if err != nil {
			return nil, herror.Wrap(err)
		}
		if !rows.Next() {
			if err := rows.Err(); err != nil {
				return nil, herror.Wrap(err)
			}
			return nil, herror.NewError("source GTID query returned no rows")
		}
		values := make([]string, len(columns))
		destinations := make([]any, len(columns))
		for i := range values {
			destinations[i] = &values[i]
		}
		if err := rows.Scan(destinations...); err != nil {
			return nil, herror.Wrap(err)
		}
		return values, nil
	}
}

// replicationGTIDQuery 将复制连接包装成 GTID 查询函数，并复制首行结果以免驱动复用底层内存。
func replicationGTIDQuery(conn mysql.Executer) gtidQuery {
	return func(query string, args ...any) ([]string, error) {
		result, err := conn.Execute(query, args...)
		if err != nil {
			return nil, herror.Wrap(err)
		}
		defer result.Close()
		if result.Resultset == nil || result.RowNumber() == 0 {
			return nil, herror.NewError("replication GTID query returned no rows")
		}
		values := make([]string, len(result.Fields))
		for i := range values {
			isNull, err := result.IsNull(0, i)
			if err != nil {
				return nil, herror.Wrap(err)
			}
			if isNull {
				return nil, herror.NewError("replication GTID query returned NULL")
			}
			value, err := result.GetString(0, i)
			if err != nil {
				return nil, herror.Wrap(err)
			}
			// GetString 可能引用驱动结果池中的底层字节。
			values[i] = strings.Clone(value)
		}
		return values, nil
	}
}

type sourceGTIDProfile struct {
	executed mysql.GTIDSet
	purged   mysql.GTIDSet
	readOnly bool
}

// gtidProfile 按源库类型读取并验证 GTID 配置、日志范围与只读状态。
func (s *Source) gtidProfile(query gtidQuery) (sourceGTIDProfile, error) {
	if s.config.Flavor == mysql.MariaDBFlavor {
		return mariaDBGTIDProfile(query)
	}
	values, err := query(`SELECT @@GLOBAL.gtid_executed, @@GLOBAL.gtid_purged, @@GLOBAL.gtid_mode,
@@GLOBAL.enforce_gtid_consistency, @@GLOBAL.log_replica_updates, @@GLOBAL.read_only, @@GLOBAL.super_read_only`)
	if err != nil {
		return sourceGTIDProfile{}, herror.WrapMsg(err, "read MySQL GTID configuration")
	}
	if len(values) != 7 || values[2] != "ON" || values[3] != "ON" || !enabled(values[4]) {
		return sourceGTIDProfile{}, herror.NewError("GTID auto-position requires MySQL GTID mode, GTID consistency, and logging replica updates")
	}
	executed, err := mysql.ParseGTIDSet(mysql.MySQLFlavor, values[0])
	if err != nil {
		return sourceGTIDProfile{}, herror.WrapMsg(err, "parse MySQL executed GTIDs")
	}
	purged, err := mysql.ParseGTIDSet(mysql.MySQLFlavor, values[1])
	if err != nil {
		return sourceGTIDProfile{}, herror.WrapMsg(err, "parse MySQL purged GTIDs")
	}
	if !executed.Contain(purged) {
		return sourceGTIDProfile{}, herror.NewError("MySQL executed GTIDs do not cover its purged GTIDs")
	}
	return sourceGTIDProfile{executed: executed, purged: purged, readOnly: enabled(values[5]) || enabled(values[6])}, nil
}

// mariaDBGTIDProfile 验证 MariaDB 的严格 GTID 模式与复制日志设置，并读取当前日志的 GTID。
func mariaDBGTIDProfile(query gtidQuery) (sourceGTIDProfile, error) {
	values, err := query(`SELECT @@GLOBAL.gtid_binlog_pos, @@GLOBAL.gtid_current_pos, @@GLOBAL.gtid_slave_pos,
@@GLOBAL.gtid_strict_mode, @@GLOBAL.log_slave_updates, @@GLOBAL.read_only`)
	if err != nil {
		return sourceGTIDProfile{}, herror.WrapMsg(err, "read MariaDB GTID configuration")
	}
	if len(values) != 6 || !enabled(values[3]) || (values[2] != "" && !enabled(values[4])) {
		return sourceGTIDProfile{}, herror.NewError("GTID auto-position requires MariaDB strict GTID mode and logging replicated transactions")
	}
	executed, err := mysql.ParseGTIDSet(mysql.MariaDBFlavor, values[0])
	if err != nil {
		return sourceGTIDProfile{}, herror.WrapMsg(err, "parse MariaDB binlog GTIDs")
	}
	current, err := mysql.ParseGTIDSet(mysql.MariaDBFlavor, values[1])
	if err != nil {
		return sourceGTIDProfile{}, herror.WrapMsg(err, "parse MariaDB current GTIDs")
	}
	if !executed.Equal(current) {
		return sourceGTIDProfile{}, herror.NewError("MariaDB current GTID position includes history not recorded in its binlog")
	}
	return sourceGTIDProfile{executed: executed, readOnly: enabled(values[5])}, nil
}

// enabled 判断服务器变量的字符串值是否表示启用。
func enabled(value string) bool {
	return value == "1" || strings.EqualFold(value, "ON")
}

// verifyGTIDResume 验证当前成员具备恢复所需的日志历史，并返回 MariaDB 仍需在订阅中核验的边界。
func (s *Source) verifyGTIDResume(query gtidQuery, durable mysql.GTIDSet) (map[uint32]mysql.MariadbGTID, error) {
	profile, err := s.gtidProfile(query)
	if err != nil {
		return nil, err
	}
	if !coversGTIDs(profile.executed, durable) {
		return nil, herror.NewError("current source does not contain the persisted recovery GTID set")
	}
	if s.config.Flavor == mysql.MariaDBFlavor {
		profile.purged, err = mariaDBRetainedGTIDs(query)
		if err != nil {
			return nil, err
		}
	}
	if !coversGTIDs(durable, profile.purged) {
		return nil, herror.NewError("current source has purged binlog history required after the persisted recovery GTID set")
	}
	if profile.readOnly {
		return nil, herror.NewError("GTID auto-position requires a writable source member")
	}
	if s.config.Flavor != mysql.MariaDBFlavor {
		return nil, nil
	}
	latest := profile.executed.(*mysql.MariadbGTIDSet)
	oldest := profile.purged.(*mysql.MariadbGTIDSet)
	boundaries := make(map[uint32]mysql.MariadbGTID)
	for domain, point := range durable.(*mysql.MariadbGTIDSet).Sets {
		if *latest.Sets[domain] == *point {
			continue
		}
		if floor := oldest.Sets[domain]; floor != nil && *floor == *point {
			continue
		}
		boundaries[domain] = *point
	}
	return boundaries, nil
}

// mariaDBRetainedGTIDs 从最早保留的 MariaDB binlog 中读取 GTID 下界。
func mariaDBRetainedGTIDs(query gtidQuery) (mysql.GTIDSet, error) {
	logs, err := query("SHOW BINARY LOGS")
	if err != nil {
		return nil, herror.WrapMsg(err, "find oldest retained MariaDB binlog")
	}
	if len(logs) == 0 || logs[0] == "" {
		return nil, herror.NewError("MariaDB has no oldest retained binlog")
	}
	position, err := query("SELECT BINLOG_GTID_POS(?, 4)", logs[0])
	if err != nil {
		return nil, herror.WrapMsg(err, "read GTID floor of oldest retained MariaDB binlog")
	}
	if len(position) != 1 {
		return nil, herror.NewError("MariaDB returned an invalid retained GTID floor")
	}
	set, err := mysql.ParseGTIDSet(mysql.MariaDBFlavor, position[0])
	if err != nil {
		return nil, herror.WrapMsg(err, "parse oldest retained MariaDB binlog GTIDs")
	}
	return set, nil
}

// coversGTIDs 判断可用集合是否覆盖所需 GTID。MariaDB 在同一域和序号上还必须具有相同服务器 ID，候选水位领先时由订阅流核验精确来源。
func coversGTIDs(available, requested mysql.GTIDSet) bool {
	if !available.Contain(requested) {
		return false
	}
	positions, ok := requested.(*mysql.MariadbGTIDSet)
	if !ok {
		return true
	}
	latest := available.(*mysql.MariadbGTIDSet)
	for domain, point := range positions.Sets {
		observed := latest.Sets[domain]
		if observed.SequenceNumber == point.SequenceNumber && observed.ServerID != point.ServerID {
			return false
		}
	}
	return true
}
