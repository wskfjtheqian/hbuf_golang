package mysqlsource

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/go-mysql-org/go-mysql/client"
	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/go-mysql-org/go-mysql/replication"
	sqlmysql "github.com/go-sql-driver/mysql"
	sqlparser "github.com/pingcap/tidb/pkg/parser"
	"github.com/pingcap/tidb/pkg/parser/ast"
	_ "github.com/pingcap/tidb/pkg/parser/test_driver"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
)

// ErrTransientStream 表示订阅因传输失败中断，可以再次校验持久游标后续订。
var ErrTransientStream = herror.NewError("source binlog transport unavailable")

// streamError 保留原始错误，仅在明确的连接故障上标记可重试。
func streamError(ctx context.Context, err error, operation string) error {
	if cause := ctx.Err(); cause != nil {
		return herror.WrapMsg(cause, operation)
	}
	var network *net.OpError
	if errors.As(err, &network) || errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, mysql.ErrBadConn) || errors.Is(err, driver.ErrBadConn) ||
		errors.Is(err, sqlmysql.ErrInvalidConn) || errors.Is(err, net.ErrClosed) {
		err = errors.Join(ErrTransientStream, err)
	}
	return herror.WrapMsg(err, operation)
}

// Stream 从已验证的恢复游标开始交付已提交事务。表清单应采用该代次保存的结构目录。
func (s *Source) Stream(ctx context.Context, cursor Cursor, tables []Table, consume func(Transaction) error) error {
	return s.StreamWithReady(ctx, cursor, tables, nil, consume)
}

// StreamWithReady 通过实际 binlog 订阅确认恢复位置，在交付首笔事务前通知调用方订阅就绪。
func (s *Source) StreamWithReady(ctx context.Context, cursor Cursor, tables []Table, ready func(), consume func(Transaction) error) error {
	return s.StreamStaged(ctx, cursor, tables, ready, nil, consume)
}

// StreamStaged 将事务中的行交给 stage 暂存，在提交后调用 consume。stage 为空时通过 Changes 交付行。
// 调用方负责在断开连接后丢弃未提交的暂存。
func (s *Source) StreamStaged(ctx context.Context, cursor Cursor, tables []Table, ready func(), stage func(Change) error, consume func(Transaction) error) error {
	preflight, cancel := context.WithTimeout(ctx, 15*time.Second)
	gtidSet, err := s.validateResume(preflight, cursor)
	cancel()
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			return errors.Join(ErrTransientStream, herror.WrapMsg(err, "verify durable binlog cursor"))
		}
		return streamError(ctx, err, "verify durable binlog cursor")
	}
	selected := make(map[TableRef]Table, len(tables))
	for _, table := range tables {
		selected[table.Ref] = table
	}
	if len(selected) != len(s.config.Tables) || len(selected) != len(tables) {
		return herror.NewError("source catalog does not match configured table selection")
	}
	for _, ref := range s.config.Tables {
		if _, exists := selected[ref]; !exists {
			return herror.NewError("source catalog is missing " + qualifiedTable(ref))
		}
	}
	var mariaBoundaries map[uint32]mysql.MariadbGTID
	var decoder *stagedBinlogDecoder
	if stage != nil {
		// 先预检原始事件，再解码行。复制库的压缩事务解码会把
		// 整笔事务放入内存，绕开采集端的逐行暂存预算。
		decoder = newStagedBinlogDecoder(s.config.Flavor)
	}
	adapter := replication.NewBinlogSyncer(replication.BinlogSyncerConfig{
		ServerID:                s.config.ServerID,
		Flavor:                  s.config.Flavor,
		Host:                    s.host,
		Port:                    s.port,
		User:                    s.config.User,
		Password:                s.config.Password,
		Charset:                 "utf8mb4",
		TimestampStringLocation: time.UTC,
		RenderJSONAsMySQLText:   true,
		DisableRetrySync:        true,
		EventCacheCount:         1,
		RawModeEnabled:          decoder != nil,
		ReadTimeout:             15 * time.Second,
		HeartbeatPeriod:         5 * time.Second,
		VerifyChecksum:          true,
		DiscardGTIDSet:          true,
		FillZeroLogPos:          s.config.Flavor == mysql.MariaDBFlavor,
		Dialer: func(dialCtx context.Context, network, address string) (net.Conn, error) {
			return dialReplication(ctx, dialCtx, network, address)
		},
		Option: func(conn *client.Conn) error {
			if err := s.verifyReplicaConnection(conn, gtidSet); err != nil {
				return err
			}
			if s.config.GTIDAutoPosition {
				var err error
				mariaBoundaries, err = s.verifyGTIDResume(replicationGTIDQuery(conn), gtidSet)
				return err
			}
			return nil
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	defer adapter.Close()

	var stream *replication.BinlogStreamer
	var position mysql.Position
	start := "start replication at durable binlog position"
	if s.config.GTIDAutoPosition {
		start = "start replication at durable GTID position"
		// 同步器维护独立的可变 GTID 集合，事务读取器只在收到提交事件后推进自己的集合。
		stream, err = adapter.StartSyncGTID(gtidSet.Clone())
	} else {
		position = mysql.Position{Name: cursor.File, Pos: cursor.Position}
		stream, err = adapter.StartSync(position)
	}
	if err != nil {
		return streamError(ctx, err, start)
	}
	file, buffered, err := confirmBinlogStream(ctx, stream, position, mariaBoundaries, decoder)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return herror.WrapMsg(err, "verify source accepted durable binlog subscription")
	}
	cursor.SourceIdentity = s.identity
	reader := transactionReader{
		file: file, cursor: cursor, committed: gtidSet, requireGTID: s.config.GTIDAutoPosition,
		tables: selected, parser: sqlparser.New(), stage: stage, consume: consume,
	}
	if s.config.GTIDAutoPosition {
		hlog.Info(ctx, "source GTID subscription started member=%s file=%s", s.identity, file)
	} else {
		hlog.Info(ctx, "source binlog subscription started file=%s position=%d", cursor.File, cursor.Position)
	}
	if ready != nil {
		ready()
	}
	accept := func(raw *replication.BinlogEvent) error {
		event, err := decoder.decode(raw)
		if err != nil {
			return err
		}
		return reader.accept(event)
	}
	for _, event := range buffered {
		if err := accept(event); err != nil {
			return err
		}
	}
	for {
		event, err := stream.GetEvent(ctx)
		if err != nil {
			return streamError(ctx, err, "read source binlog event")
		}
		if err := accept(event); err != nil {
			return err
		}
	}
}

// confirmBinlogStream 核验订阅流返回的起始位置与日志格式，并暂存 GTID 边界确认期间收到的事件。
func confirmBinlogStream(ctx context.Context, stream *replication.BinlogStreamer, position mysql.Position, mariaBoundaries map[uint32]mysql.MariadbGTID, decoder *stagedBinlogDecoder) (string, []*replication.BinlogEvent, error) {
	startup, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	first, err := stream.GetEvent(startup)
	if err != nil {
		return "", nil, streamHandshakeError(ctx, err, "verify source accepted durable binlog subscription")
	}
	rotate, ok := first.Event.(*replication.RotateEvent)
	if !ok || first.Header.Timestamp != 0 || rotate.Position < 4 || len(rotate.NextLogName) == 0 {
		return "", nil, herror.NewError("subscription did not identify its starting binlog file")
	}
	file := string(rotate.NextLogName)
	if position.Name != "" && (file != position.Name || rotate.Position != uint64(position.Pos)) {
		return "", nil, herror.NewError(fmt.Sprintf("source started at binlog %s:%d instead of durable position %s:%d", file, rotate.Position, position.Name, position.Pos))
	}
	second, err := stream.GetEvent(startup)
	if err != nil {
		return "", nil, streamHandshakeError(ctx, err, "read subscription binlog format")
	}
	if _, ok := second.Event.(*replication.FormatDescriptionEvent); !ok {
		return "", nil, herror.NewError("subscription did not declare its binlog format")
	}
	if _, err := decoder.decode(second); err != nil {
		return "", nil, err
	}
	// MariaDB 可能先接受订阅请求，再在保留的日志中定位精确 GTID 边界。
	// 由事务组后的 GTID 列表或文件开头的 GTID 列表核验域、服务器来源和序号。
	// 所有边界确认之前，暂存其他域的事件。
	const maxStartupEvents = 4096
	const maxStartupBytes = 64 << 20
	var buffered []*replication.BinlogEvent
	var bytes uint64
	for len(mariaBoundaries) > 0 {
		event, err := stream.GetEvent(startup)
		if err != nil {
			return "", nil, streamError(ctx, err, "verify exact MariaDB GTID boundaries in the source binlog")
		}
		if event.Header.EventType == replication.MARIADB_GTID_LIST_EVENT {
			event, err = decoder.decode(event)
			if err != nil {
				return "", nil, err
			}
		}
		if list, ok := event.Event.(*replication.MariadbGTIDListEvent); ok {
			for _, observed := range list.GTIDs {
				if point, pending := mariaBoundaries[observed.DomainID]; pending && observed == point {
					delete(mariaBoundaries, observed.DomainID)
				}
			}
			continue
		}
		bytes += uint64(event.Header.EventSize)
		if len(buffered) == maxStartupEvents || bytes > maxStartupBytes {
			return "", nil, herror.NewError("MariaDB GTID boundary verification exceeded its startup buffer")
		}
		buffered = append(buffered, event)
	}
	return file, buffered, nil
}

// streamHandshakeError 将订阅首帧超时视作传输故障。精确 GTID 边界校验仍使用独立错误处理。
func streamHandshakeError(ctx context.Context, err error, operation string) error {
	if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
		err = errors.Join(ErrTransientStream, err)
	}
	return streamError(ctx, err, operation)
}

type replicationConn struct {
	net.Conn
	stopCancel func() bool
}

// Close 取消复制连接的上下文回调并关闭网络连接。
func (c *replicationConn) Close() error {
	c.stopCancel()
	return c.Conn.Close()
}

// dialReplication 建立继承采集上下文生命周期的复制连接。
func dialReplication(ctx, dialCtx context.Context, network, address string) (net.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dialCtx, cancel := context.WithCancel(dialCtx)
	defer cancel()
	stop := context.AfterFunc(ctx, cancel)
	defer stop()
	dialer := net.Dialer{}
	conn, err := dialer.DialContext(dialCtx, network, address)
	if err != nil {
		return nil, err
	}
	// 复制库在登录后会取消拨号上下文。采集上下文负责登录、注册和订阅期间的连接生命周期。
	return &replicationConn{Conn: conn, stopCancel: context.AfterFunc(ctx, func() { _ = conn.Close() })}, nil
}

// verifyReplicaConnection 核对复制连接与 SQL 连接是否指向同一源成员，并验证适用的 GTID 历史。
func (s *Source) verifyReplicaConnection(conn *client.Conn, committed mysql.GTIDSet) error {
	query := "SELECT @@GLOBAL.server_uuid"
	if s.config.Flavor == mysql.MariaDBFlavor {
		query = "SELECT @@GLOBAL.server_id"
	}
	result, err := conn.Execute(query)
	if err != nil {
		return herror.WrapMsg(err, "verify replication connection source identity")
	}
	defer result.Close()
	value, err := result.GetString(0, 0)
	if err != nil {
		return herror.WrapMsg(err, "read replication connection source identity")
	}
	identity := s.config.SourceIdentity + "/" + value
	if s.config.Flavor == mysql.MariaDBFlavor {
		identity = s.config.SourceIdentity + "/" + s.config.Address + "/" + value
	}
	if identity != s.identity {
		return herror.NewError("replication connection source identity differs from SQL source")
	}
	if s.config.GTIDAutoPosition {
		return nil
	}
	if committed.String() == "" {
		return nil
	}
	query = "SELECT @@GLOBAL.gtid_executed"
	if s.config.Flavor == mysql.MariaDBFlavor {
		query = "SELECT @@GLOBAL.gtid_binlog_pos"
	}
	result, err = conn.Execute(query)
	if err != nil {
		return herror.WrapMsg(err, "verify recovery GTID set against source")
	}
	defer result.Close()
	value, err = result.GetString(0, 0)
	if err != nil {
		return herror.WrapMsg(err, "read current source GTID set")
	}
	sourceSet, err := mysql.ParseGTIDSet(s.config.Flavor, value)
	if err != nil {
		return herror.WrapMsg(err, "parse current source GTID set")
	}
	if !sourceSet.Contain(committed) {
		return herror.NewError("current source does not contain the persisted recovery GTID set")
	}
	return nil
}

type transactionReader struct {
	file        string
	cursor      Cursor
	committed   mysql.GTIDSet
	requireGTID bool

	active      bool
	standalone  bool
	pendingGTID string
	changes     []Change
	staged      bool
	tables      map[TableRef]Table
	parser      *sqlparser.Parser
	stage       func(Change) error
	consume     func(Transaction) error
}

// accept 将 binlog 事件分派给对应解析流程，并在事务提交边界发布记录。
func (r *transactionReader) accept(event *replication.BinlogEvent) error {
	switch e := event.Event.(type) {
	case *replication.RotateEvent:
		nextFile := string(e.NextLogName)
		if r.active || r.pendingGTID != "" || r.staged {
			return r.unsupported(event, "binlog rotated within an incomplete transaction")
		}
		if event.Header.Timestamp == 0 && nextFile != r.file && !r.requireGTID {
			return r.unsupported(event, "replication server returned a different starting binlog file")
		}
		r.file = nextFile
	case *replication.GtidTaggedLogEvent:
		return r.beginGTID(event, &e.GTIDEvent, false)
	case *replication.GTIDEvent:
		if event.Header.EventType == replication.ANONYMOUS_GTID_EVENT {
			if r.requireGTID {
				return r.unsupported(event, "anonymous transaction cannot be resumed by GTID")
			}
			if r.active || r.pendingGTID != "" || r.staged {
				return r.unsupported(event, "anonymous GTID appeared within an incomplete transaction")
			}
			return nil
		}
		return r.beginGTID(event, e, false)
	case *replication.MariadbGTIDEvent:
		if err := r.beginGTID(event, e, e.IsStandalone() || e.IsDDL()); err != nil {
			return err
		}
		// MariaDB 用事务 GTID 事件代替 BEGIN 语句。
		r.active = !r.standalone
		return nil
	case *replication.RowsEvent:
		return r.acceptRows(event, e)
	case *replication.XIDEvent:
		return r.commit(event.Header.LogPos, false)
	case *replication.QueryEvent:
		return r.acceptQuery(event, e)
	case *replication.TransactionPayloadEvent:
		if len(e.Events) == 0 || e.Events[len(e.Events)-1].Header.EventType != replication.XID_EVENT {
			return r.unsupported(event, "unsupported compressed transaction ending")
		}
		for _, inner := range e.Events[:len(e.Events)-1] {
			switch innerEvent := inner.Event.(type) {
			case *replication.QueryEvent:
				if strings.ToUpper(strings.TrimSpace(string(innerEvent.Query))) != "BEGIN" {
					return r.unsupported(event, "unsupported query in compressed transaction")
				}
			case *replication.TableMapEvent, *replication.RowsEvent, *replication.RowsQueryEvent:
			default:
				return r.unsupported(event, "unsupported event in compressed transaction")
			}
			if err := r.accept(inner); err != nil {
				return err
			}
		}
		return r.commit(event.Header.LogPos, false)
	default:
		switch event.Header.EventType {
		case replication.FORMAT_DESCRIPTION_EVENT, replication.TABLE_MAP_EVENT, replication.PREVIOUS_GTIDS_EVENT,
			replication.HEARTBEAT_EVENT, replication.HEARTBEAT_LOG_EVENT_V2, replication.STOP_EVENT,
			replication.ROWS_QUERY_EVENT, replication.MARIADB_GTID_LIST_EVENT, replication.MARIADB_BINLOG_CHECKPOINT_EVENT,
			replication.MARIADB_ANNOTATE_ROWS_EVENT:
			return nil
		default:
			return r.unsupported(event, "unsupported source binlog event")
		}
	}
	return nil
}

// beginGTID 检查事务边界，并记录下一笔事务的 GTID 和独立事务标记。
func (r *transactionReader) beginGTID(event *replication.BinlogEvent, gtid mysql.BinlogGTIDEvent, standalone bool) error {
	if r.active || r.pendingGTID != "" || r.staged {
		return r.unsupported(event, "source GTID appeared within an incomplete transaction")
	}
	next, err := gtid.GTIDNext()
	if err != nil {
		return herror.WrapMsg(err, "parse source transaction GTID")
	}
	if next.String() == "" {
		return r.unsupported(event, "source GTID is empty")
	}
	if r.requireGTID && r.committed.Contain(next) {
		return r.unsupported(event, "GTID subscription repeated or regressed a committed transaction")
	}
	r.pendingGTID, r.standalone = next.String(), standalone
	return nil
}

// acceptRows 根据订阅表的历史结构解码并暂存行变更。
func (r *transactionReader) acceptRows(event *replication.BinlogEvent, rows *replication.RowsEvent) error {
	if rows.Table == nil {
		return r.unsupported(event, "binlog row event has no table map")
	}
	ref := TableRef{Schema: string(rows.Table.Schema), Name: string(rows.Table.Table)}
	table, selected := r.tables[ref]
	if !selected {
		if r.standalone && rows.Flags&replication.STMT_END_F != 0 {
			return r.commit(event.Header.LogPos, false)
		}
		return nil
	}
	if !r.active || r.standalone {
		return r.unsupported(event, "selected row event has no supported transaction boundary")
	}
	if rows.ColumnCount != uint64(len(table.Columns)) || rows.Table.ColumnCount != rows.ColumnCount {
		return r.unsupported(event, "binlog row column count differs from snapshot catalog")
	}
	if names := rows.Table.ColumnNameString(); len(names) != 0 {
		if len(names) != len(table.Columns) {
			return r.unsupported(event, "binlog table map has incomplete column names")
		}
		for index, name := range names {
			if name != table.Columns[index].Name {
				return r.unsupported(event, "binlog table map differs from snapshot column order")
			}
		}
	}
	signedness := rows.Table.UnsignedMap()
	for index, column := range table.Columns {
		if known, nullable := rows.Table.Nullable(index); known && nullable != column.Nullable {
			return r.unsupported(event, "binlog table map nullability differs from historical schema")
		}
		// binlog 可能为 YEAR 列附带符号位标志，SQL 会忽略该标志。
		if unsigned, known := signedness[index]; known && column.DataType != "year" && unsigned != isUnsigned(column) {
			return r.unsupported(event, "binlog table map signedness differs from historical schema")
		}
	}
	if event.Header.EventType == replication.PARTIAL_UPDATE_ROWS_EVENT {
		return r.unsupported(event, "binlog partial JSON row image is unsupported")
	}
	for _, skipped := range rows.SkippedColumns {
		if len(skipped) != 0 {
			return r.unsupported(event, "binlog row image omits source columns")
		}
	}
	for _, image := range rows.Rows {
		if len(image) != len(table.Columns) {
			return r.unsupported(event, "binlog row image has invalid width")
		}
		for i, column := range table.Columns {
			image[i] = normalizeUnsigned(column, image[i])
		}
	}

	switch rows.Type() {
	case replication.EnumRowsEventTypeInsert:
		for _, image := range rows.Rows {
			if err := r.stageChange(Change{Table: ref, After: image}); err != nil {
				return err
			}
		}
	case replication.EnumRowsEventTypeDelete:
		for _, image := range rows.Rows {
			if err := r.stageChange(Change{Table: ref, Before: image}); err != nil {
				return err
			}
		}
	case replication.EnumRowsEventTypeUpdate:
		if len(rows.Rows)%2 != 0 {
			return r.unsupported(event, "binlog update has incomplete row image pair")
		}
		for i := 0; i < len(rows.Rows); i += 2 {
			if err := r.stageChange(Change{Table: ref, Before: rows.Rows[i], After: rows.Rows[i+1]}); err != nil {
				return err
			}
		}
	default:
		return r.unsupported(event, "unknown binlog row change action")
	}
	return nil
}

// stageChange 通过外部暂存行变更，或为调用方保留完整事务的行清单。
func (r *transactionReader) stageChange(change Change) error {
	r.staged = true
	if r.stage != nil {
		return r.stage(change)
	}
	r.changes = append(r.changes, change)
	return nil
}

// normalizeUnsigned 按列位宽还原无符号整数的原始值。
func normalizeUnsigned(column Column, value any) any {
	if !isUnsigned(column) {
		return value
	}
	switch value := value.(type) {
	case int8:
		return uint8(value)
	case int16:
		return uint16(value)
	case int32:
		if column.DataType == "mediumint" {
			return uint32(value) & 0xffffff
		}
		return uint32(value)
	case int64:
		return uint64(value)
	default:
		return value
	}
}

// isUnsigned 根据列定义判断该列是否为无符号整数。
func isUnsigned(column Column) bool {
	return strings.Contains(strings.ToLower(column.ColumnType), " unsigned")
}

// acceptQuery 处理事务控制语句与源端 DDL，并对无法安全解释的变更生成屏障。
func (r *transactionReader) acceptQuery(event *replication.BinlogEvent, query *replication.QueryEvent) error {
	statementText := string(query.Query)
	// MySQL 和 MariaDB 执行版本化注释的方式可能与当前解析器不同。
	// 遇到这类注释时阻止自动规划 ALTER，以免遗漏注释中的变更。
	if strings.Contains(statementText, "/*!") || strings.Contains(statementText, "/*M!") || strings.Contains(statementText, "/*m!") {
		const reason = "source SQL contains a potentially executable comment"
		if !r.active && query.ErrorCode == 0 {
			if barrier := r.firstBlockedTable(reason, func(TableRef) bool { return true }); barrier != nil {
				return r.publish(event.Header.LogPos, false, barrier)
			}
		}
		return r.unsupported(event, reason)
	}
	statements, _, err := r.parser.Parse(statementText, "", "")
	if err != nil || len(statements) != 1 || query.ErrorCode != 0 {
		return r.unsupported(event, "unrecognized or failed source SQL event")
	}
	switch statement := statements[0].(type) {
	case *ast.BeginStmt:
		if r.active || r.standalone {
			return r.unsupported(event, "unexpected BEGIN in source transaction")
		}
		r.active = true
		return nil
	case *ast.CommitStmt:
		return r.commit(event.Header.LogPos, false)
	case *ast.RollbackStmt:
		if statement.SavepointName != "" {
			return r.unsupported(event, "ROLLBACK TO SAVEPOINT is unsupported")
		}
		return r.commit(event.Header.LogPos, true)
	case *ast.SavepointStmt, *ast.ReleaseSavepointStmt:
		if r.active {
			return nil
		}
	}
	if !r.active {
		schema := string(query.Schema)
		change := r.planDDL(schema, statements[0])
		if change != nil {
			if change.Kind == DDLAddColumns && len(change.AddedColumns) == 0 {
				return r.commit(event.Header.LogPos, false)
			}
			return r.publish(event.Header.LogPos, false, change)
		}
		if r.ignorableDDL(schema, statements[0]) {
			return r.commit(event.Header.LogPos, false)
		}
	}
	return r.unsupported(event, "source SQL requires an unsupported DDL or statement barrier")
}

// ignorableDDL 仅在已知受影响表均不属于订阅范围时允许跳过 DDL。对无法判定范围的语句暂停采集。
func (r *transactionReader) ignorableDDL(defaultSchema string, statement ast.StmtNode) bool {
	excluded := func(table *ast.TableName) bool {
		schema := table.Schema.O
		if schema == "" {
			schema = defaultSchema
		}
		if schema == "" {
			return false
		}
		for ref := range r.tables {
			if strings.EqualFold(ref.Schema, schema) && strings.EqualFold(ref.Name, table.Name.O) {
				return false
			}
		}
		return true
	}
	excludedSchema := func(schema string) bool {
		if schema == "" {
			return false
		}
		for ref := range r.tables {
			if strings.EqualFold(ref.Schema, schema) {
				return false
			}
		}
		return true
	}
	switch statement := statement.(type) {
	case *ast.CreateTableStmt:
		return excluded(statement.Table)
	case *ast.AlterTableStmt:
		// ALTER TABLE 可能把未订阅的表重命名到订阅范围内，也可能与订阅表交换分区。
		// 遇到这些形式时暂停采集。
		for _, spec := range statement.Specs {
			if spec.NewTable != nil {
				return false
			}
		}
		return excluded(statement.Table)
	case *ast.DropTableStmt:
		for _, table := range statement.Tables {
			if !excluded(table) {
				return false
			}
		}
		return true
	case *ast.TruncateTableStmt:
		return excluded(statement.Table)
	case *ast.RenameTableStmt:
		for _, pair := range statement.TableToTables {
			if !excluded(pair.OldTable) || !excluded(pair.NewTable) {
				return false
			}
		}
		return true
	case *ast.CreateIndexStmt:
		return excluded(statement.Table)
	case *ast.DropIndexStmt:
		return excluded(statement.Table)
	case *ast.CreateDatabaseStmt:
		return excludedSchema(statement.Name.O)
	case *ast.DropDatabaseStmt:
		return excludedSchema(statement.Name.O)
	case *ast.AlterDatabaseStmt:
		schema := statement.Name.O
		if schema == "" {
			schema = defaultSchema
		}
		return excludedSchema(schema)
	default:
		return false
	}
}

// commit 在事务提交或回滚边界发布记录。
func (r *transactionReader) commit(position uint32, rollback bool) error {
	return r.publish(position, rollback, nil)
}

// publish 更新物理游标和 GTID 集合，交付事务或 DDL 后更新历史结构及事务状态。
func (r *transactionReader) publish(position uint32, rollback bool, ddl *DDLChange) error {
	if position < 4 || r.file == "" {
		return herror.NewError("source commit has no usable physical binlog cursor")
	}
	id := r.pendingGTID
	if id == "" {
		if r.requireGTID {
			return herror.NewError("source commit has no GTID in an auto-position subscription")
		}
		id = r.file + ":" + strconv.FormatUint(uint64(position), 10)
	} else if err := r.committed.Update(id); err != nil {
		return herror.WrapMsg(err, "advance committed source GTID set")
	}
	r.cursor.File, r.cursor.Position, r.cursor.GTIDSet = r.file, position, r.committed.String()
	changes := r.changes
	if rollback {
		changes = nil
	}
	if err := r.consume(Transaction{ID: id, Cursor: r.cursor, Changes: changes, DDL: ddl, RolledBack: rollback}); err != nil {
		return err
	}
	if ddl != nil {
		if ddl.Kind == DDLUnsupported {
			return herror.NewError("source DDL barrier cannot be applied: " + ddl.Reason)
		}
		r.tables[ddl.After.Ref] = ddl.After
	}
	r.active, r.standalone, r.staged, r.pendingGTID, r.changes = false, false, false, "", nil
	return nil
}

// unsupported 为不支持的事件构建包含日志文件、位置和事件类型的错误。
func (r *transactionReader) unsupported(event *replication.BinlogEvent, message string) error {
	return herror.NewError(fmt.Sprintf("%s file=%s position=%d event=%s",
		message, r.file, event.Header.LogPos, event.Header.EventType))
}
