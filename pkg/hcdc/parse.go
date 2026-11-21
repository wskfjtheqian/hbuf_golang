package hcdc

import (
	"strings"

	"github.com/pingcap/tidb/pkg/parser"
	"github.com/pingcap/tidb/pkg/parser/ast"
	"github.com/pingcap/tidb/pkg/parser/format"
	"github.com/pingcap/tidb/pkg/parser/types"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hutl"
)

// ParseDDL 解析 DDL 语句，返回受影响表的结构信息。基于 TiDB parser 的 AST
// （与 canal/sync.go 的 parseStmt 同源），替代旧的正则解析：
//   - CREATE TABLE / ALTER TABLE / DROP TABLE / TRUNCATE 返回对应 TableInfo；
//   - ALTER 时提取列变更（ADD/MODIFY/CHANGE/DROP/RENAME COLUMN）；
//   - 其他语句（RENAME TABLE、CREATE/DROP INDEX、CREATE TRIGGER、DROP VIEW 等）
//     与解析失败均返回 nil，与旧正则行为一致。
//
// QueryEvent 可能携带多条语句，这里逐条解析并返回第一个命中的表 DDL。
func ParseDDL(schema Schema, sql string) *TableInfo {
	// DDL 事件低频，按调用创建 parser 实例（无共享状态，避免并发复用问题）。
	stmts, _, err := parser.New().Parse(strings.TrimSpace(sql), "", "")
	if err != nil {
		return nil
	}
	for _, stmt := range stmts {
		if infos := ParseDDLStmts(schema, stmt); len(infos) > 0 {
			return infos[0]
		}
	}
	return nil
}

// ParseDDLStmts 把 fork（canal/sync.go）已解析的单条 AST 语句展开为受影响表的
// TableInfo 列表，供 OnDDL 直接复用，避免对同一 SQL 二次解析。
// schema 为 QueryEvent 携带的默认库，语句中未限定库的表名使用它。
// 非表结构 DDL 返回空列表。
func ParseDDLStmts(schema Schema, stmt ast.StmtNode) []*TableInfo {
	switch t := stmt.(type) {
	case *ast.CreateTableStmt:
		// CREATE 的 AST 已携带完整列定义，直接提取 Columns/Keys/分区字段，
		// 无需回源查询（见 createTableToInfo）。
		return []*TableInfo{createTableToInfo(schema, t)}
	case *ast.AlterTableStmt:
		return []*TableInfo{
			{
				Schema:  tableSchema(schema, t.Table),
				Table:   Table(t.Table.Name.String()),
				Columns: extractAlterColumns(t),
				Action:  Alter,
			},
		}
	case *ast.DropTableStmt:
		// DROP VIEW 与 DROP TABLE 共用 DropTableStmt（IsView 区分），
		// 旧正则只匹配 DROP TABLE，这里保持行为一致：忽略 DROP VIEW。
		if t.IsView {
			return nil
		}
		infos := make([]*TableInfo, 0, len(t.Tables))
		for _, table := range t.Tables {
			infos = append(infos, &TableInfo{
				Schema: tableSchema(schema, table),
				Table:  Table(table.Name.String()),
				Action: Drop,
			})
		}
		return infos
	case *ast.TruncateTableStmt:
		return []*TableInfo{
			{
				Schema: tableSchema(schema, t.Table),
				Table:  Table(t.Table.Name.String()),
				Action: Truncate,
			},
		}
	default:
		// RENAME TABLE、CREATE/DROP INDEX 等非表结构变更：与旧正则行为一致，忽略。
		return nil
	}
}

// tableSchema 解析表名携带的库名，未限定时回退 QueryEvent 的默认库。
func tableSchema(schema Schema, t *ast.TableName) Schema {
	if t.Schema.String() != "" {
		return Schema(t.Schema.String())
	}
	return schema
}

// createTableToInfo 把 CREATE TABLE 的 AST 直接转换为完整 TableInfo，与
// GetTableInfo（SHOW CREATE TABLE + ParseDDL）的提取语义对齐：
//   - Columns 按定义顺序，KeyIndex 主键第一列最大，排序后主键列排最前；
//   - Keys 为排序后的主键列名（Doris UNIQUE KEY 顺序与源表主键一致）；
//   - PartitionField 为 RANGE 分区时主键中第一个 datetime/timestamp 列（与
//     GetTableInfo 的启发式一致，供 Doris AUTO PARTITION BY RANGE 使用）。
//
// DEFAULT/COMMENT 由 canal 包注入的 valueExpr（expr.go）从 AST 还原，无需回源查询。
func createTableToInfo(schema Schema, t *ast.CreateTableStmt) *TableInfo {
	info := &TableInfo{
		Schema:  tableSchema(schema, t.Table),
		Table:   Table(t.Table.Name.String()),
		Action:  Create,
		Columns: make([]ColumnInfo, 0, len(t.Cols)),
		Keys:    make([]Column, 0),
	}
	// 主键列（定义顺序）：表级 PRIMARY KEY 约束 + 列级 PRIMARY KEY 选项。
	var pk []Column
	for _, con := range t.Constraints {
		if con.Tp != ast.ConstraintPrimaryKey {
			continue
		}
		for _, key := range con.Keys {
			if key.Column != nil {
				pk = append(pk, Column(key.Column.Name.String()))
			}
		}
	}
	for _, col := range t.Cols {
		for _, opt := range col.Options {
			if opt.Tp == ast.ColumnOptionPrimaryKey {
				pk = append(pk, Column(col.Name.Name.String()))
			}
		}
	}
	// KeyIndex 与 GetKeys 语义一致：主键第一列赋最大值 len(pk)，排序后顺序正确。
	pkIndex := make(map[Column]int, len(pk))
	for i, name := range pk {
		pkIndex[name] = len(pk) - i
	}
	for _, col := range t.Cols {
		c := columnToInfo(col, "")
		c.KeyIndex = pkIndex[c.Name]
		info.Columns = append(info.Columns, c)
	}
	// 主键列排最前（与 GetTableInfo 排序后一致），保持主键定义顺序。
	hutl.Sort(info.Columns, func(i, j ColumnInfo) bool {
		return i.KeyIndex > j.KeyIndex
	})
	info.Index = hutl.SliceToMap(info.Columns, func(i int, column ColumnInfo) (Column, int) {
		return column.Name, i
	})
	for _, column := range info.Columns {
		if column.KeyIndex > 0 {
			info.Keys = append(info.Keys, column.Name)
		}
	}
	// RANGE 分区：主键中第一个 datetime/timestamp 列作为 Doris 自动分区字段
	// （与 GetTableInfo 的启发式一致；其他分区类型下游不消费，保持空值）。
	if t.Partition != nil && t.Partition.Tp == ast.PartitionTypeRange {
		info.PartitionType = "RANGE"
		for _, key := range info.Keys {
			column := info.Columns[info.Index[key]]
			if strings.HasPrefix(column.Type, "datetime") || strings.HasPrefix(column.Type, "timestamp") {
				info.PartitionField = column.Name
				break
			}
		}
	}
	return info
}

// extractAlterColumns 提取 ALTER TABLE 中的列变更。与旧正则支持的
// ADD/MODIFY/CHANGE/DROP COLUMN 对齐，并额外覆盖 RENAME COLUMN。
func extractAlterColumns(t *ast.AlterTableStmt) []ColumnInfo {
	var changes []ColumnInfo
	for _, spec := range t.Specs {
		switch spec.Tp {
		case ast.AlterTableAddColumns:
			for _, col := range spec.NewColumns {
				changes = append(changes, columnToInfo(col, "ADD"))
			}
		case ast.AlterTableModifyColumn:
			for _, col := range spec.NewColumns {
				changes = append(changes, columnToInfo(col, "MODIFY"))
			}
		case ast.AlterTableChangeColumn:
			for _, col := range spec.NewColumns {
				change := columnToInfo(col, "CHANGE")
				if spec.OldColumnName != nil {
					change.OldName = Column(spec.OldColumnName.Name.String())
				}
				changes = append(changes, change)
			}
		case ast.AlterTableDropColumn:
			if spec.OldColumnName != nil {
				changes = append(changes, ColumnInfo{
					Name:   Column(spec.OldColumnName.Name.String()),
					Action: "DROP",
					IsNull: "YES",
				})
			}
		case ast.AlterTableRenameColumn:
			// RENAME COLUMN 语法不携带类型信息，用 CHANGE 语义记录新旧列名。
			if spec.OldColumnName != nil && spec.NewColumnName != nil {
				changes = append(changes, ColumnInfo{
					OldName: Column(spec.OldColumnName.Name.String()),
					Name:    Column(spec.NewColumnName.Name.String()),
					Action:  "CHANGE",
					IsNull:  "YES",
				})
			}
		}
	}
	return changes
}

// columnToInfo 将 AST 列定义转换为 ColumnInfo。Type/Args 与
// INFORMATION_SCHEMA 的 DATA_TYPE/COLUMN_TYPE 语义对齐（如 varchar + "(255)"），
// Charset 与 CHARACTER_SET_NAME 对齐（数值类型的 binary 归一化为空）。
// DEFAULT/COMMENT 通过 canal 包注入的 valueExpr 实现（expr.go）从 AST 还原。
func columnToInfo(col *ast.ColumnDef, action string) ColumnInfo {
	c := ColumnInfo{
		Name:   Column(col.Name.Name.String()),
		Action: action,
		IsNull: "YES",
	}
	if col.Tp != nil {
		c.Type, c.Args = splitFieldType(col.Tp)
		// 字符集从 AST 单独提取存入 Charset（而非混进 Args）：Doris 建表不支持
		// charset 子句但源信息值得保留。数值/二进制类型的 binary 无意义，置空。
		if cs := col.Tp.GetCharset(); cs != "" && cs != "binary" {
			c.Charset = cs
		}
	}
	for _, opt := range col.Options {
		switch opt.Tp {
		case ast.ColumnOptionNotNull:
			c.IsNull = "NO"
		case ast.ColumnOptionDefaultValue:
			if d := restoreExpr(opt.Expr); d != "" {
				c.Default = &d
			}
		case ast.ColumnOptionComment:
			c.Comment = restoreComment(opt.Expr)
		}
	}
	return c
}

// splitFieldType 把 AST 字段类型拆成 Type（小写类型名）与 Args（参数部分），
// 与 INFORMATION_SCHEMA.COLUMNS 的 DATA_TYPE/COLUMN_TYPE 语义对齐（如
// "varchar" + "(255)"）。
// 用 CompactStr()（仅类型名 + 参数）：FieldType.String() 会附加 UNSIGNED/
// ZEROFILL/BINARY/CHARACTER SET/COLLATE 修饰子句，Doris 均不支持，直接用
// CompactStr 从源头避免修饰混入 Args（建表 SQL 透传会导致 Doris 语法报错）。
func splitFieldType(tp *types.FieldType) (string, string) {
	s := strings.ToLower(tp.CompactStr())
	typ := types.TypeToStr(tp.GetType(), tp.GetCharset())
	return typ, strings.TrimPrefix(s, typ)
}

// restoreExpr 把 AST 表达式还原为 SQL 文本（用于 DEFAULT 值与 COMMENT）。
func restoreExpr(expr ast.ExprNode) string {
	if expr == nil {
		return ""
	}
	var sb strings.Builder
	rc := format.NewRestoreCtx(format.DefaultRestoreFlags, &sb)
	if err := expr.Restore(rc); err != nil {
		return ""
	}
	return sb.String()
}

// restoreComment 与 INFORMATION_SCHEMA.COLUMN_COMMENT 语义对齐：返回去引号的
// 原始文本（下游 doris.go 建表时自行加引号，带 restore 引号会双重包裹）。
func restoreComment(expr ast.ExprNode) string {
	if v, ok := expr.(ast.ValueExpr); ok {
		return v.GetString()
	}
	return restoreExpr(expr)
}
