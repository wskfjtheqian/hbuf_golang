package hcdc

import (
	"context"
	"encoding/base64"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"database/sql"

	"github.com/RoaringBitmap/roaring"
	"github.com/RoaringBitmap/roaring/roaring64"
	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/go-mysql-org/go-mysql/replication"
	"github.com/go-mysql-org/go-mysql/schema"
	_ "github.com/go-sql-driver/mysql"
	"github.com/pingcap/tidb/pkg/parser/ast"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc/canal"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hutl"
)

type OnData func(ctx context.Context, schema Schema, table Table, action Action, columns []ColumnInfo, values [][]RawBytes) error
type OnCreateTable func(ctx context.Context, infos []TableInfo) error
type OnCreateSchema func(ctx context.Context, schema Schema) error

// SyncPosition 断点续传位点：物理位点（binlog 文件 + offset）加上可选的 GTID 集合。
// Gtid 为空表示源库未开启 GTID，恢复时回退用物理位点 RunFrom。
type SyncPosition struct {
	Position mysql.Position
	Gtid     string
}

// OnGetPosition 查询指定实例上次成功上传的 binlog 位点（含 GTID），用于断点续传。
// 返回 nil 表示无记录（首次启动）。由 SetConfig 注入，内部委托 Doris.GetPosition。
type OnGetPosition func(ctx context.Context, instanceId InstanceId) (*SyncPosition, error)

type CanalConfig struct {
	InstanceId    InstanceId `yaml:"InstanceId"`    //实例ID
	Host          string     `yaml:"Host"`          //数据库主机地址
	Username      string     `yaml:"Username"`      //数据库用户名
	Password      string     `yaml:"Password"`      //数据库密码
	Schema        string     `yaml:"Schema"`        //数据库名称
	IncludeDBs    []string   `yaml:"IncludeDBs"`    //支持的库
	ExcludeDBs    []string   `yaml:"ExcludeDBs"`    //排除的库
	IncludeTables []string   `yaml:"IncludeTables"` //支持的表
	ExcludeTables []string   `yaml:"ExcludeTables"` //排除的表
	ServerID      *uint32    `yaml:"ServerID"`      //服务器ID
	Charset       string     `yaml:"Charset"`       //字符集
	Flavor        string     `yaml:"Flavor"`        //数据库类型
	LogDir        string     `yaml:"LogDir"`        //日志目录
}

func (c *CanalConfig) Validate(ctx context.Context) bool {
	if c == nil {
		hlog.Error(ctx, "canal config is nil")
		return false
	}
	if strings.TrimSpace(c.Host) == "" {
		hlog.Error(ctx, "canal config host is empty")
		return false
	}
	if strings.TrimSpace(c.Username) == "" {
		hlog.Error(ctx, "canal config username is empty")
		return false
	}
	if strings.TrimSpace(c.Schema) == "" {
		hlog.Error(ctx, "canal config schema is empty")
		return false
	}
	// ServerID 未显式配置时 go-mysql 会随机生成，多实例部署可能冲突。
	// 生产环境建议显式配置一个全局唯一的 serverID（用于 binlog 订阅的 replica 标识）。
	if c.ServerID == nil {
		hlog.Warn(ctx, "canal config serverID is not set, go-mysql will generate a random one; "+
			"set a globally unique serverID for production to avoid replica id conflict")
	}
	return true
}

func (c *CanalConfig) Equal(other *CanalConfig) bool {
	if c == nil && other == nil {
		return true
	}
	if c == nil || other == nil {
		return false
	}
	if len(c.IncludeDBs) != len(other.IncludeDBs) {
		return false
	}
	for i := range c.IncludeDBs {
		if c.IncludeDBs[i] != other.IncludeDBs[i] {
			return false
		}
	}

	if len(c.ExcludeDBs) != len(other.ExcludeDBs) {
		return false
	}
	for i := range c.ExcludeDBs {
		if c.ExcludeDBs[i] != other.ExcludeDBs[i] {
			return false
		}
	}

	if len(c.IncludeTables) != len(other.IncludeTables) {
		return false
	}
	for i := range c.IncludeTables {
		if c.IncludeTables[i] != other.IncludeTables[i] {
			return false
		}
	}

	if len(c.ExcludeTables) != len(other.ExcludeTables) {
		return false
	}
	for i := range c.ExcludeTables {
		if c.ExcludeTables[i] != other.ExcludeTables[i] {
			return false
		}
	}

	return c.Host == other.Host &&
		c.Username == other.Username &&
		c.Password == other.Password &&
		c.Schema == other.Schema &&
		c.Charset == other.Charset &&
		c.Flavor == other.Flavor
}

func NewCanal(cfg *CanalConfig) *Canal {
	ret := &Canal{
		cfg:     cfg,
		schemas: make(map[Schema]map[Table]TableInfo),
		worker:  NewWorker(cfg.InstanceId, cfg.LogDir),
	}
	return ret
}

type Canal struct {
	canal.DummyEventHandler // 嵌入空实现的事件处理器，按需覆盖
	cfg                     *CanalConfig
	canal                   *canal.Canal
	//conn                    *client.Conn
	excludeDBs     []*regexp.Regexp
	includeDBs     []*regexp.Regexp
	excludeTables  []*regexp.Regexp
	includeTables  []*regexp.Regexp
	onCreateTable  OnCreateTable
	onCreateSchema OnCreateSchema
	onGetPosition  OnGetPosition
	schemas        map[Schema]map[Table]TableInfo
	lock           sync.Mutex
	sql            *sql.DB
	worker         *Worker
	monitor        *Monitor
	gtidSet        mysql.GTIDSet // 已采集事务的 GTID 集合，仅在 RunFrom goroutine 内串行读写

	// 同步生命周期：cancel 用于在 Close 时终止后台同步 goroutine 与重连循环。
	cancel context.CancelFunc
}

func (c *Canal) setMonitor(m *Monitor) {
	c.monitor = m
}

func (c *Canal) setOnCreateTable(fn OnCreateTable) {
	c.onCreateTable = fn
}

func (c *Canal) setOnCreateSchema(fn OnCreateSchema) {
	c.onCreateSchema = fn
}

func (c *Canal) setOnGetPosition(fn OnGetPosition) {
	c.onGetPosition = fn
}

func (c *Canal) Open(ctx context.Context) error {
	err := c.initFilter(ctx)
	if err != nil {
		return err
	}

	// 建立直连，用于执行查询和锁表操作。
	c.sql, err = sql.Open("mysql", c.cfg.Username+":"+c.cfg.Password+"@tcp("+c.cfg.Host+")/"+c.cfg.Schema)
	if err != nil {
		return err
	}
	// 连接池参数：限制并发连接数、空闲连接数与连接生命周期，避免连接泄漏。
	c.sql.SetMaxOpenConns(8)
	c.sql.SetMaxIdleConns(4)
	c.sql.SetConnMaxLifetime(5 * time.Minute)

	// 建立 canal 实例（需在取位点之前，GetMasterPos 依赖它）。
	c.canal, err = c.newCanal()
	if err != nil {
		return err
	}

	// 建立表元数据（OnRow 依赖 c.schemas 查找列结构），并在 Doris 侧建库建表。
	// createSchemaTable 内部走 CREATE IF NOT EXISTS，幂等，断点续传时也可安全重跑。
	err = c.createSchemaTable(ctx)
	if err != nil {
		return err
	}

	// 决定同步起点：
	//  1. 有上次已上传位点 → 从该位点断点续传，跳过全量拉取（避免重复全量）。
	//  2. 无位点（首次启动）→ 先在全量拉取之前记录当前位点，全量拉取完成后从该位点开始，
	//     这样全量拉取期间源库产生的新增 DML 会被随后的 binlog 重放，不会丢失。
	last := (*SyncPosition)(nil)
	if c.onGetPosition != nil {
		if p, err := c.onGetPosition(ctx, c.cfg.InstanceId); err != nil {
			return herror.Wrap(err)
		} else if p != nil {
			last = p
		}
	}

	if last == nil {
		// 首次启动：先记录全量起始位点，再做全量拉取。
		pos, err := c.canal.GetMasterPos()
		if err != nil {
			return herror.Wrap(err)
		}
		if err = c.loadData(ctx); err != nil {
			return err
		}
		last = &SyncPosition{Position: pos}
		hlog.Info(ctx, "full load done, start incremental from: %s:%d", pos.Name, pos.Pos)
	} else {
		hlog.Info(ctx, "resume from persisted position: %s:%d gtid=%s", last.Position.Name, last.Position.Pos, last.Gtid)
	}

	// 后台启动同步循环（含断线重连）。使用独立 context，Close 时通过 cancel 终止。
	syncCtx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	go c.runSync(syncCtx, *last)
	return nil
}

// newCanal 创建 go-mysql canal 实例并注册事件处理器。
func (c *Canal) newCanal() (*canal.Canal, error) {
	cfg := canal.NewDefaultConfig()
	if c.cfg.ServerID != nil {
		cfg.ServerID = *c.cfg.ServerID
	}
	cfg.Addr = c.cfg.Host
	cfg.User = c.cfg.Username
	cfg.Password = c.cfg.Password
	cfg.Charset = c.cfg.Charset
	// 仅当显式配置时才覆盖默认 flavor（NewDefaultConfig 默认 "mysql"），
	// 否则空字符串会覆盖默认值，导致 MariaDB 等场景 GTID 无法解析。
	if c.cfg.Flavor != "" {
		cfg.Flavor = c.cfg.Flavor
	}
	cfg.TimestampStringLocation = time.UTC
	cfg.IncludeTableRegex = c.cfg.IncludeTables
	cfg.ExcludeTableRegex = c.cfg.ExcludeTables

	cInst, err := canal.NewCanal(cfg)
	if err != nil {
		return nil, herror.Wrap(err)
	}
	cInst.SetEventHandler(c)
	return cInst, nil
}

// runSync 后台同步循环：从给定初始位点（优先 GTID）启动 binlog 订阅，失败后指数退避并重建
// canal 实例重连（go-mysql 的 canal 实例失败后不可复用，须重新创建）。
// 初始位点由 Open 传入（全量起始位点或断点续传位点）；重连时则重新查询持久化位点。
func (c *Canal) runSync(ctx context.Context, initial SyncPosition) {
	backoff := time.Second
	const maxBackoff = 30 * time.Second

	pos := initial
	// StartFromGTID 被服务端拒绝（或 GTID 集解析失败）后置位，本进程内一律改走 Position
	// 续传。位点表可能持久化了源库未开启 GTID 时的匿名占位集（全零 SID），服务端会拒绝
	// （ERROR 1773），不置位则重连后再次采纳同一坏 GTID 形成死循环。
	gtidUnusable := false
	for {
		if ctx.Err() != nil {
			return
		}

		cInst := c.canal
		var runErr error
		if pos.Gtid != "" && !gtidUnusable {
			if gtidSet, parseErr := mysql.ParseGTIDSet(c.canalCfgFlavor(), pos.Gtid); parseErr == nil {
				hlog.Info(ctx, "start sync from persisted gtid: %s", pos.Gtid)
				runErr = cInst.StartFromGTID(gtidSet)
				if runErr != nil {
					hlog.Error(ctx, "start sync from gtid failed (%v), fallback to position %s:%d",
						runErr, pos.Position.Name, pos.Position.Pos)
					gtidUnusable = true
					runErr = cInst.RunFrom(pos.Position)
				}
			} else {
				hlog.Error(ctx, "parse persisted gtid failed (%v), fallback to position", parseErr)
				gtidUnusable = true
				runErr = cInst.RunFrom(pos.Position)
			}
		} else {
			runErr = cInst.RunFrom(pos.Position)
		}

		if runErr == nil {
			// RunFrom/StartFromGTID 正常返回通常是 canal 已关闭（Close 触发），正常退出。
			return
		}
		if ctx.Err() != nil {
			return
		}

		hlog.Error(ctx, "canal sync error: %v, will reconnect after %v", runErr, backoff)

		// 重建 canal 实例（旧的已不可复用）。
		if nc, err := c.newCanal(); err != nil {
			hlog.Error(ctx, "recreate canal failed: %v", err)
		} else {
			if c.canal != nil {
				c.canal.Close()
			}
			c.canal = nc
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff < maxBackoff {
			backoff *= 2
		}

		// 重连前重新查询上次已上传位点（可能已被 scanFile 持续推进）。
		if p := c.syncedPosition(ctx); p.Position.Name != "" || p.Gtid != "" {
			if gtidUnusable {
				p.Gtid = "" // GTID 已被证明不可用，仅采纳 Position
			}
			pos = p
		}
	}
}

// syncedPosition 返回当前应作为同步起点的位点：优先从 Doris 持久化位点读取（断点续传），
// 读不到时回退到 canal 当前 synced 位点（避免从零开始）。
func (c *Canal) syncedPosition(ctx context.Context) SyncPosition {
	if c.onGetPosition != nil {
		if p, err := c.onGetPosition(ctx, c.cfg.InstanceId); err == nil && p != nil {
			return *p
		}
	}
	if c.canal != nil {
		return SyncPosition{Position: c.canal.SyncedPosition()}
	}
	return SyncPosition{}
}

// canalCfgFlavor 返回用于 GTID 解析的 flavor（与 newCanal 应用默认值后一致）。
func (c *Canal) canalCfgFlavor() string {
	if c.cfg.Flavor != "" {
		return c.cfg.Flavor
	}
	return mysql.MySQLFlavor
}

// OnDDL 当上游执行了 CREATE TABLE、ALTER TABLE、DROP TABLE、TRUNCATE 等 SQL 语句时触发。
// stmt 是 fork 在 handleEvent 中已解析的 AST 语句（完整数据结构），直接用
// ParseDDLStmts 展开受影响表，不再对同一 SQL 二次解析。
//   - TRUNCATE：需要位点信息（nextPos），让 worker 落一个独立控制文件，先清理该表
//     此前积压的全部 .temp/.active（TRUNCATE 前的数据本就要清空，无需上传），
//     再由上传端识别控制文件、对 Doris 执行 TRUNCATE TABLE。与 OnRow 共用一个
//     addData channel，保证 TRUNCATE 与前后 DML 严格串行。
//   - DROP：只需要表名即可，清理本地结构缓存并通知 Doris 侧删表。
//   - CREATE/ALTER：回源库查最新完整表结构（ALTER 的 AST 只携带列变更，
//     不足以在 Doris 侧正确建表/改表），写回本地缓存后通知 Doris 侧建表/改表。
func (c *Canal) OnDDL(header *replication.EventHeader, nextPos mysql.Position, queryEvent *replication.QueryEvent, stmt ast.StmtNode) error {
	ctx := hlog.NewContext()
	infos := ParseDDLStmts(Schema(queryEvent.Schema), stmt)
	for _, info := range infos {
		// 与全量拉取保持一致，只处理命中过滤规则的表。
		if !c.FilterDatabase(info.Schema) || !c.FilterTable(info.Table) {
			continue
		}
		if info.Action == Truncate {
			binlogName := nextPos.Name
			if binlogName == "" {
				binlogName = c.canal.SyncedPosition().Name
			}
			gtid := ""
			if c.gtidSet != nil {
				gtid = normalizeGTID(c.gtidSet.String())
			} else if gset := c.canal.SyncedGTIDSet(); gset != nil {
				gtid = normalizeGTID(gset.String())
			}
			return c.worker.Truncate(ctx, info.Schema, info.Table, mysql.Position{
				Name: binlogName,
				Pos:  nextPos.Pos,
			}, gtid)
		} else if info.Action == Drop {
			// DROP 只需要表名即可：清理本地结构缓存，并通知 Doris 侧删表。
			c.removeTableInfo(info.Schema, info.Table)
			if c.onCreateTable == nil {
				continue
			}
			if err := c.onCreateTable(ctx, []TableInfo{*info}); err != nil {
				return err
			}
			continue
		} else if info.Action == Alter {
			// ALTER 的 AST 只携带列变更（extractAlterColumns），不足以在 Doris 侧
			// 正确建表/改表，回源库查最新完整结构。
			full, err := c.GetTableInfo(ctx, info.Schema, info.Table)
			if err != nil {
				return err
			}
			if full == nil {
				continue
			}
			// 将最新表结构写回本地缓存，避免 OnRow 用旧结构做列映射（DDL 后缓存滞后）。
			c.cacheTableInfo(info.Schema, info.Table, *full)
			if c.onCreateTable == nil {
				continue
			}
			if err := c.onCreateTable(ctx, []TableInfo{*full}); err != nil {
				return err
			}
		} else if info.Action == Create {
			// CREATE 的 AST 已携带完整列定义，ParseDDLStmts 已提取 Columns/Keys/
			// 分区字段，直接使用提取的结构，无需回源查询。
			c.cacheTableInfo(info.Schema, info.Table, *info)
			if c.onCreateTable == nil {
				continue
			}
			if err := c.onCreateTable(ctx, []TableInfo{*info}); err != nil {
				return err
			}
		}
	}
	return nil
}

// OnPosSynced 位点同步回调。当前位点（binlog 文件名 + offset）已由 OnRow 通过
// c.canal.SyncedPosition() 实时获取并随数据写入 CSV 文件，最终由 Doris.scanFile
// 持久化到 cdc_record.log_position。这里无需再缓存到内存字段（避免 Close 与
// RunFrom 两个 goroutine 并发写同一字段的数据竞争）。
func (c *Canal) OnPosSynced(header *replication.EventHeader, pos mysql.Position, set mysql.GTIDSet, force bool) error {
	return nil
}

// OnGTID 在收到 GTID 事件时被调用，用于累积「已采集事务」的 GTID 集合。
// 注意：go-mysql 对 MariaDB 的 MariadbGTIDEvent 只触发本回调、并不更新 master 的
// GTID set（SyncedGTIDSet 因此恒为 nil），所以必须在这里自行累积。
// 本方法与 OnRow 都在 RunFrom 的同步 goroutine 内串行执行，无并发写入。
func (c *Canal) OnGTID(header *replication.EventHeader, e mysql.BinlogGTIDEvent) error {
	if e == nil {
		return nil
	}
	next, err := e.GTIDNext()
	if err != nil || next == nil {
		return nil
	}
	// 源库未开启 GTID 时事件流仍可能携带「SID 全零、GNO=0」的匿名占位事件（go-mysql
	// 解码行为）。累积它会把匿名占位集持久化到位点表，重启续传时 StartFromGTID
	// 被服务端拒绝（ERROR 1773）导致重连死循环，直接忽略。
	if normalizeGTID(next.String()) == "" {
		return nil
	}
	if c.gtidSet == nil {
		c.gtidSet = next.Clone()
	} else if err := c.gtidSet.Update(next.String()); err != nil {
		return nil
	}
	return nil
}

func (c *Canal) OnRow(e *canal.RowsEvent) error {
	val, ok := c.schemas[Schema(e.Table.Schema)]
	if !ok || val == nil {
		return nil
	}
	info, ok := val[Table(e.Table.Name)]
	if !ok {
		return nil
	}

	ctx := hlog.NewContext()

	// 列名来源：优先用 binlog 自带的权威列名（TableMapEvent.ColumnName，需
	// binlog_row_metadata=FULL），为空时回退 canal 表缓存列名。彻底摆脱「DDL 后
	// 缓存滞后导致列名映射错位」的问题。
	names := e.ColumnNames
	if len(names) == 0 {
		names = hutl.Slice(e.Table.Columns, func(i int, v schema.TableColumn) string { return v.Name })
	}

	// 权威列数（binlog TableMapEvent.ColumnCount）：行值按此解码，列数不一致说明
	// 缓存滞后（如 INSTANT ADD COLUMN），必须中止同步触发缓存刷新与重放，否则会越界。
	columnCount := e.ColumnCount
	if columnCount == 0 {
		columnCount = len(names)
	}

	// 按权威列名定位缓存的 ColumnInfo（列名在缓存查不到说明结构已变，刷新缓存并中止）。
	columnInfos := make([]ColumnInfo, 0, columnCount)
	for _, name := range names {
		idx, ok := info.Index[Column(name)]
		if !ok {
			c.refreshTableMeta(ctx, e.Table.Schema, e.Table.Name)
			return fmt.Errorf("hcdc: table %s.%s: binlog column %q not found in cached schema (schema changed?), cache refreshed, resync pending",
				e.Table.Schema, e.Table.Name, name)
		}
		columnInfos = append(columnInfos, info.Columns[idx])
	}

	// 位点文件名用 SyncedPosition().Name：它在 RunFrom(startPos) 时即被正确初始化为
	// 真实 binlog 文件名；若依赖 OnPosSynced 缓存，首个事务的 OnRow 时尚未更新会是空，
	// 导致写入 cdc_record.log_position 的 file 为空、断点续传失败。
	binlogName := c.canal.SyncedPosition().Name
	// GTID 用于断点续传（优先于物理位点）。优先用 OnGTID 累积的 c.gtidSet（覆盖
	// MySQL 与 MariaDB），回退 SyncedGTIDSet（仅 MySQL XID 路径会更新 master gset）。
	gtid := ""
	if c.gtidSet != nil {
		gtid = normalizeGTID(c.gtidSet.String())
	} else if gset := c.canal.SyncedGTIDSet(); gset != nil {
		gtid = normalizeGTID(gset.String())
	}
	// 关键：go-mysql 的一个 RowsEvent 可能携带多行数据（尤其批量 INSERT/多行 UPDATE/DELETE），
	// e.Rows 是 [][]any：insert/delete 每行一条记录；update 是 [before, after, before, after, ...] 成对。
	// 必须遍历全部行，否则批量 DML 会静默丢行（只同步首行）。
	convertRow := func(vals []any) ([]RawBytes, error) {
		// 行值列数必须与 binlog 权威列数一致（full row image），不一致说明缓存滞后，
		// 直接索引会越界，须中止同步触发缓存刷新与重放。
		if len(vals) != columnCount {
			c.refreshTableMeta(ctx, e.Table.Schema, e.Table.Name)
			return nil, fmt.Errorf("hcdc: table %s.%s: row has %d values but binlog declares %d columns (schema changed?), cache refreshed, resync pending",
				e.Table.Schema, e.Table.Name, len(vals), columnCount)
		}
		row := make([]RawBytes, 0, len(vals))
		for i, v := range vals {
			var bt byte
			if i < len(e.ColumnTypes) {
				bt = e.ColumnTypes[i]
			}
			row = append(row, c.toRawBytes(ctx, &columnInfos[i], bt, v))
		}
		return row, nil
	}

	var err error
	if e.Action == "insert" || e.Action == "delete" {
		rows := make([][]RawBytes, 0, len(e.Rows))
		for _, r := range e.Rows {
			row, convErr := convertRow(r)
			if convErr != nil {
				c.recordCapture(ctx, convErr, nil)
				return convErr
			}
			rows = append(rows, row)
		}
		action := Insert
		if e.Action == "delete" {
			action = Delete
		}
		err = c.worker.AddData(ctx, Schema(e.Table.Schema), Table(e.Table.Name), action, columnInfos, rows, mysql.Position{Pos: e.Header.LogPos, Name: binlogName}, gtid)
		c.recordCapture(ctx, err, rows)
		return err
	} else if e.Action == "update" {
		// update 的 Rows 成对：[before, after, before, after, ...]，取 after 行（索引 1,3,5...）。
		rows := make([][]RawBytes, 0, len(e.Rows)/2)
		for i := 1; i < len(e.Rows); i += 2 {
			row, convErr := convertRow(e.Rows[i])
			if convErr != nil {
				c.recordCapture(ctx, convErr, nil)
				return convErr
			}
			rows = append(rows, row)
		}
		err = c.worker.AddData(ctx, Schema(e.Table.Schema), Table(e.Table.Name), Update, columnInfos, rows, mysql.Position{Pos: e.Header.LogPos, Name: binlogName}, gtid)
		c.recordCapture(ctx, err, rows)
		return err
	}

	return nil
}

// refreshTableMeta 刷新指定表的元数据缓存（DDL 后自愈）：
//   - 清除 go-mysql canal 的表缓存，下一个事件触发 GetTable 重新从源库查询；
//   - 重新查询源表结构并写回 hcdc 本地 c.schemas（OnRow 的列映射依赖它）。
//
// 与 OnRow/OnDDL 在 canal 同步 goroutine 内串行调用；cacheTableInfo 内部加锁保护。
func (c *Canal) refreshTableMeta(ctx context.Context, schemaName string, tableName string) {
	if c.canal != nil {
		c.canal.ClearTableCache([]byte(schemaName), []byte(tableName))
	}
	info, err := c.GetTableInfo(ctx, Schema(schemaName), Table(tableName))
	if err != nil || info == nil {
		return
	}
	c.cacheTableInfo(Schema(schemaName), Table(tableName), *info)
}

// cacheTableInfo 把最新表结构写回本地缓存（加锁）。
func (c *Canal) cacheTableInfo(schema Schema, table Table, info TableInfo) {
	c.lock.Lock()
	defer c.lock.Unlock()
	if _, ok := c.schemas[schema]; !ok {
		c.schemas[schema] = make(map[Table]TableInfo)
	}
	c.schemas[schema][table] = info
}

// removeTableInfo 从本地结构缓存移除已删除的表，避免 OnRow 用残留结构继续采集。
func (c *Canal) removeTableInfo(schema Schema, table Table) {
	c.lock.Lock()
	defer c.lock.Unlock()
	if tables, ok := c.schemas[schema]; ok {
		delete(tables, table)
	}
}

// recordCapture 记录采集端指标（行数与字节数，失败时计数错误）。
func (c *Canal) recordCapture(ctx context.Context, err error, rows [][]RawBytes) {
	if c.monitor == nil {
		return
	}
	if err != nil {
		c.monitor.CaptureErrors.Add(1)
		return
	}
	var bytes int64
	for _, row := range rows {
		c.monitor.CaptureRows.Add(1)
		for _, v := range row {
			bytes += int64(len(v))
		}
	}
	c.monitor.CaptureBytes.Add(bytes)
}

// normalizeGTID 过滤「源库未开启 GTID」时的匿名占位 GTID 集（SID 全零、GNO=0）。
// 占位集无断点续传意义，统一返回空字符串（与 cdc_record.log_position 表「gtid 为空
// 表示未开启 GTID」的语义一致），避免重启续传时 StartFromGTID 被服务端拒绝
// （ERROR 1773）导致重连死循环。
func normalizeGTID(gtid string) string {
	if strings.HasPrefix(gtid, "00000000-0000-0000-0000-000000000000:") {
		return ""
	}
	return gtid
}

// noBinlogType 表示「无 binlog 类型信息」的哨兵值（全量 SELECT 路径）。
// 不能用 0：go-mysql 里 MYSQL_TYPE_DECIMAL == 0（iota 从 0 起），会被 binlogTypeKind
// 误判为 decimal。0xFF 不在任何合法 MYSQL_TYPE_* 枚举值内，是安全的哨兵。
const noBinlogType byte = 0xFF

// binlogTypeKind 把 binlog TableMapEvent 的 MYSQL_TYPE_*（byte）映射为与 ColumnInfo.Type
// （INFORMATION_SCHEMA.DATA_TYPE 小写字符串）对齐的语义分类，供 toRawBytes 判断是否直通/base64/
// 日期替换。tp == noBinlogType 表示无 binlog 类型信息（全量 SELECT 路径），此时上层应回退使用缓存 col.Type。
//
// 这是「跟着 binlog 元数据走」的关键：类型语义判断完全脱离可能滞后的表结构缓存。
func binlogTypeKind(tp byte) string {
	switch tp {
	case mysql.MYSQL_TYPE_DATE, mysql.MYSQL_TYPE_NEWDATE:
		return "date"
	case mysql.MYSQL_TYPE_DATETIME, mysql.MYSQL_TYPE_DATETIME2:
		return "datetime"
	case mysql.MYSQL_TYPE_TIMESTAMP, mysql.MYSQL_TYPE_TIMESTAMP2:
		return "timestamp"
	case mysql.MYSQL_TYPE_TIME, mysql.MYSQL_TYPE_TIME2:
		return "time"
	case mysql.MYSQL_TYPE_YEAR:
		return "year"
	case mysql.MYSQL_TYPE_DECIMAL, mysql.MYSQL_TYPE_NEWDECIMAL:
		return "decimal"
	default:
		return ""
	}
}

// isPassthroughType 判断列类型是否「透传列」——CSV header 中不加 _base、值不做 base64，
// 与 file.go 里 header 生成的透传列清单保持一致。数值/日期/时间/年份等类型都属于透传列。
func isPassthroughType(typ string) bool {
	switch typ {
	case "boolean", "bool", "decimal", "numeric", "double", "real", "float",
		"tinyint", "smallint", "int", "integer", "mediumint", "bigint", "largeint",
		"time", "date", "datetime", "timestamp", "year":
		return true
	}
	return false
}

func (c *Canal) toRawBytes(ctx context.Context, col *ColumnInfo, binlogType byte, v any) RawBytes {
	if v == nil {
		return nil
	}
	if reflect.TypeOf(v).Kind() == reflect.Ptr {
		v = reflect.ValueOf(v).Elem().Interface()
	}
	if v == nil {
		return nil
	}
	switch v.(type) {
	case int8:
		return RawBytes(strconv.FormatInt(int64(v.(int8)), 10))
	case int16:
		return RawBytes(strconv.FormatInt(int64(v.(int16)), 10))
	case int32:
		return RawBytes(strconv.FormatInt(int64(v.(int32)), 10))
	case int64:
		return RawBytes(strconv.FormatInt(int64(v.(int64)), 10))
	case uint8:
		return RawBytes(strconv.FormatInt(int64(v.(uint8)), 10))
	case uint16:
		return RawBytes(strconv.FormatInt(int64(v.(uint16)), 10))
	case uint32:
		return RawBytes(strconv.FormatInt(int64(v.(uint32)), 10))
	case uint64:
		return RawBytes(strconv.FormatInt(int64(v.(uint64)), 10))
	case int:
		return RawBytes(strconv.FormatInt(int64(v.(int)), 10))
	case float32:
		return RawBytes(strconv.FormatFloat(float64(v.(float32)), 'f', -1, 64))
	case float64:
		return RawBytes(strconv.FormatFloat(v.(float64), 'f', -1, 64))
	case []byte:
		val := v.([]byte)
		// 优先用 binlog 权威类型；tp==noBinlogType（全量路径）回退缓存类型。
		typ := binlogTypeKind(binlogType)
		if typ == "" {
			typ = GetColumnType(col)
		}
		if typ == "date" || typ == "datetime" || typ == "timestamp" {
			return RawBytes(strings.ReplaceAll(string(val), "0000-00-00", "1970-01-01"))
		} else if isPassthroughType(typ) {
			// 数值/日期/时间/年份类型在 CSV header 中为透传列（不加 _base），必须直通、不 base64。
			// 全量路径 Scan 到 *interface{} 时数值列也是 []byte（如 "20"），须按原样透传，
			// 否则会被错误 base64 成 "MjA=" 导致 Doris 端类型不匹配、行被过滤。
			return val
		} else if typ == "bitmap32" || typ == "bitmap64" {
			val = c.bitmapToArrayString(ctx, val, strings.Contains(col.Comment, "CustomType=Bitmap32"))
		}
		return RawBytes(base64.StdEncoding.EncodeToString([]byte(val)))
	case string:
		val := v.(string)
		typ := binlogTypeKind(binlogType)
		if typ == "" {
			typ = GetColumnType(col)
		}
		if typ == "date" || typ == "datetime" || typ == "timestamp" {
			return RawBytes(strings.ReplaceAll(val, "0000-00-00", "1970-01-01"))
		} else if isPassthroughType(typ) {
			return RawBytes(val)
		} else if typ == "bitmap32" || typ == "bitmap64" {
			val = string(c.bitmapToArrayString(ctx, []byte(val), strings.Contains(col.Comment, "CustomType=Bitmap32")))
		}
		return RawBytes(base64.StdEncoding.EncodeToString([]byte(val)))
	default:
		return RawBytes(base64.StdEncoding.EncodeToString([]byte(v.(string))))
	}
}

// 转换 Bitmap
func (c *Canal) bitmapToArrayString(ctx context.Context, val []byte, bitmap32 bool) RawBytes {
	buffer := make([]byte, 0, 512)
	if bitmap32 {
		bitmap := roaring.NewBitmap()
		_, err := bitmap.FromUnsafeBytes(val)
		if err != nil {
			hlog.Error(ctx, "bitmap from bytes error: %v", err)
			return nil
		}
		iterator := bitmap.Iterator()
		for iterator.HasNext() {
			buffer = append(buffer, strconv.FormatUint(uint64(iterator.Next()), 10)...)
			buffer = append(buffer, ',')
		}
	} else {
		bitmap := roaring64.NewBitmap()
		_, err := bitmap.FromUnsafeBytes(val)
		if err != nil {
			hlog.Error(ctx, "bitmap from bytes error: %v", err)
			return nil
		}
		iterator := bitmap.Iterator()
		for iterator.HasNext() {
			buffer = append(buffer, strconv.FormatUint(iterator.Next(), 10)...)
			buffer = append(buffer, ',')
		}
	}
	if len(buffer) > 0 {
		buffer = buffer[:len(buffer)-1]
	}
	return buffer
}

// OnTableNotFound 作用： 当 Binlog 里存在某张表的数据变更，但是 Canal 试图去上游查询该表的元数据（列名、类型）时发现表已经不存在（可能被暴力 DROP 了）。业务实现： 应该捕获并记录高级别告警日志，防止程序 Panic，同时可在本地删除对应的 Doris Worker 映射。
func (c *Canal) OnTableNotFound(header *replication.EventHeader, event *replication.RowsEvent) error {
	// 打印告警：上游对应的表可能已经被物理删除
	return nil
}

// initFilter 初始化过滤器
func (c *Canal) initFilter(ctx context.Context) error {
	for _, item := range c.cfg.ExcludeDBs {
		compile, err := regexp.Compile(item)
		if err != nil {
			return err
		}
		c.excludeDBs = append(c.excludeDBs, compile)
	}
	for _, item := range c.cfg.IncludeDBs {
		compile, err := regexp.Compile(item)
		if err != nil {
			return err
		}
		c.includeDBs = append(c.includeDBs, compile)
	}
	for _, item := range c.cfg.ExcludeTables {
		compile, err := regexp.Compile(item)
		if err != nil {
			return err
		}
		c.excludeTables = append(c.excludeTables, compile)
	}
	for _, item := range c.cfg.IncludeTables {
		compile, err := regexp.Compile(item)
		if err != nil {
			return err
		}
		c.includeTables = append(c.includeTables, compile)
	}
	return nil
}

// GetDatabases 获得所有的库
func (c *Canal) GetDatabases(ctx context.Context) ([]Schema, error) {
	//result, err := c.conn.Execute("SHOW DATABASES")
	//if err != nil {
	//	return nil, herror.Wrap(err)
	//}
	//defer result.Close()
	//
	//var dbs []string
	//for _, rows := range result.Values {
	//	dbs = append(dbs, string(rows[0].AsString()))
	//}

	query, err := c.sql.QueryContext(ctx, "SHOW DATABASES")
	if err != nil {
		return nil, herror.Wrap(err)
	}
	defer query.Close()

	var dbs []Schema
	for query.Next() {
		var db string
		if err := query.Scan(&db); err != nil {
			return nil, herror.Wrap(err)
		}
		dbs = append(dbs, Schema(db))
	}
	if err := query.Err(); err != nil {
		return nil, herror.Wrap(err)
	}
	return dbs, nil
}

// systemSchemas 是 MySQL/MariaDB 的内置系统库，全量同步时应默认跳过，
// 避免同步到 Doris 后因库名是保留字或内容无业务意义而报错/污染。
var systemSchemas = map[Schema]struct{}{
	"information_schema": {},
	"performance_schema": {},
	"mysql":              {},
	"sys":                {},
}

// isSystemSchema 判断是否为系统库。
func isSystemSchema(name Schema) bool {
	_, ok := systemSchemas[Schema(strings.ToLower(string(name)))]
	return ok
}

// FilterDatabase 过滤库：系统库与 exclude 优先排除；include 为空表示不限制，否则仅放行命中的库。
func (c *Canal) FilterDatabase(name Schema) bool {
	if isSystemSchema(name) {
		return false
	}
	for _, exclude := range c.excludeDBs {
		if exclude.MatchString(string(name)) {
			return false
		}
	}
	if len(c.includeDBs) == 0 {
		return true
	}
	for _, include := range c.includeDBs {
		if include.MatchString(string(name)) {
			return true
		}
	}
	return false
}

// GetTables 获得所有的表
func (c *Canal) GetTables(ctx context.Context, schema Schema) ([]Table, error) {
	// 1. 将 schema 拼入 SQL，确保只查目标库。注意：如果 schema 包含特殊字符，建议用反引号包裹 `schema`
	//query := "SHOW TABLES FROM `" + string(schema) + "`"
	//result, err := c.conn.Execute(query)
	//if err != nil {
	//	return nil, herror.Wrap(err)
	//}
	//defer result.Close()
	//
	//var tables []string
	//for _, rows := range result.Values {
	//	// 2. 增加防御性代码：确保这一行有数据，防止索引越界或空指针导致 panic
	//	if len(rows) > 0 {
	//		tables = append(tables, string(rows[0].AsString()))
	//	}
	//}

	query, err := c.sql.QueryContext(ctx, "SHOW TABLES FROM `"+string(schema)+"`")
	if err != nil {
		return nil, herror.Wrap(err)
	}
	defer query.Close()

	var tables []Table
	for query.Next() {
		var table string
		if err := query.Scan(&table); err != nil {
			return nil, herror.Wrap(err)
		}
		tables = append(tables, Table(table))
	}
	if err := query.Err(); err != nil {
		return nil, herror.Wrap(err)
	}

	return tables, nil
}

// FilterTable 过滤表：exclude 优先排除；include 为空表示不限制，否则仅放行命中的表。
func (c *Canal) FilterTable(name Table) bool {
	for _, exclude := range c.excludeTables {
		if exclude.MatchString(string(name)) {
			return false
		}
	}
	if len(c.includeTables) == 0 {
		return true
	}
	for _, include := range c.includeTables {
		if include.MatchString(string(name)) {
			return true
		}
	}
	return false
}

// GetTableInfo 获得指定表的结构。
// 通过 SHOW CREATE TABLE 拿建表语句后用 tidb parser 的 AST 直接提取
// （与 OnDDL 的 CREATE 分支共用 createTableToInfo），一次查询替代原来的
// INFORMATION_SCHEMA 三次查询（COLUMNS/KEY_COLUMN_USAGE/PARTITIONS）。
func (c *Canal) GetTableInfo(ctx context.Context, schema Schema, table Table) (*TableInfo, error) {
	var name, createSQL string
	if err := c.sql.QueryRowContext(ctx, "SHOW CREATE TABLE `"+strings.ReplaceAll(string(schema), "`", "``")+"`.`"+strings.ReplaceAll(string(table), "`", "``")+"`").Scan(&name, &createSQL); err != nil {
		return nil, herror.Wrap(err)
	}
	hlog.Info(ctx, "get table info: %s.%s", schema, table)
	info := ParseDDL(schema, createSQL)
	if info == nil {
		return nil, herror.NewError("parse create table failed: " + string(schema) + "." + string(table))
	}
	return info, nil
}

func (c *Canal) ReadData(ctx context.Context, schema Schema, table Table, info TableInfo, start, end string) error {
	var key Column = "id"
	for _, column := range info.Columns {
		if column.KeyIndex > 0 {
			key = column.Name
			break
		}
	}

	fields := make([]ColumnInfo, 0)
	batch := hutl.NewBatchProcess(500, func(values [][]RawBytes) error {
		// 全量拉取无真实 binlog 位点（Name="SELECT" 标记），GTID 留空。
		return c.worker.AddData(ctx, schema, table, Insert, fields, values, mysql.Position{Pos: uint32(time.Now().Unix()), Name: "SELECT"}, "")
	})

	// start 为空表示「无下界」，全量拉取首段（覆盖主键最小值，如 id=0 或负主键）。
	// 否则用 (start, end] 半开区间做分页，避免遗漏/重复边界行。
	var (
		query *sql.Rows
		qerr  error
	)
	if start == "" {
		query, qerr = c.sql.QueryContext(ctx, "SELECT * FROM `"+string(schema)+"`.`"+string(table)+"` WHERE `"+string(key)+"` <= ?", end)
	} else {
		query, qerr = c.sql.QueryContext(ctx, "SELECT * FROM `"+string(schema)+"`.`"+string(table)+"` WHERE `"+string(key)+"` > ? AND `"+string(key)+"` <= ?", start, end)
	}
	if qerr != nil {
		return herror.Wrap(qerr)
	}
	defer query.Close()
	columns, err := query.Columns()
	if err != nil {
		return herror.Wrap(err)
	}
	for _, field := range columns {
		fields = append(fields, info.Columns[info.Index[Column(field)]])
	}

	for query.Next() {
		var values = hutl.Slice(make([]any, len(fields)), func(i int, v any) any {
			return &v
		})
		if err := query.Scan(values...); err != nil {
			return herror.Wrap(err)
		}
		if err := batch.AddData(hutl.Slice(values, func(i int, v any) RawBytes {
			return c.toRawBytes(ctx, &info.Columns[info.Index[Column(columns[i])]], noBinlogType, v)
		})); err != nil {
			return herror.Wrap(err)
		}
	}
	if err := query.Err(); err != nil {
		return herror.Wrap(err)
	}

	return batch.Finish()
}

func (c *Canal) Close() {
	// 先终止后台同步循环与重连循环，再关闭 canal 与直连。
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	if c.canal != nil {
		c.canal.Close()
		c.canal = nil
	}
	if c.sql != nil {
		_ = c.sql.Close()
		c.sql = nil
	}
}

func (c *Canal) createSchemaTable(ctx context.Context) error {
	if c.onCreateSchema == nil {
		return nil
	}
	c.lock.Lock()
	defer c.lock.Unlock()
	dbs, err := c.GetDatabases(ctx)
	if err != nil {
		return err
	}
	dbs = hutl.Filter(dbs, func(v Schema) bool {
		return c.FilterDatabase(v)
	})

	infos := make([]TableInfo, 0)
	for _, db := range dbs {
		err = c.onCreateSchema(ctx, db)
		if err != nil {
			return err
		}

		tables, err := c.GetTables(ctx, db)
		if err != nil {
			return err
		}

		tables = hutl.Filter(tables, func(v Table) bool {
			return c.FilterTable(v)
		})
		if _, ok := c.schemas[db]; !ok {
			c.schemas[db] = make(map[Table]TableInfo)
		}
		for _, table := range tables {
			info, err := c.GetTableInfo(ctx, db, table)
			if err != nil {
				return err
			}
			if info != nil {
				c.schemas[db][table] = *info
				infos = append(infos, *info)
			}
		}
	}
	if len(infos) == 0 {
		return nil
	}
	return c.onCreateTable(ctx, infos)
}

func (c *Canal) loadData(ctx context.Context) error {
	for schema, tables := range c.schemas {
		for table := range tables {
			err := c.ReadData(ctx, schema, table, c.schemas[schema][table], "", "9223372036854775807")
			if err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Canal) GetWorker() *Worker {
	return c.worker
}
