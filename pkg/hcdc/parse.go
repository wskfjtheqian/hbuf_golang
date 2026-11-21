package hcdc

import (
	"strings"

	"github.com/pingcap/tidb/pkg/parser"
	"github.com/pingcap/tidb/pkg/parser/ast"
	"github.com/pingcap/tidb/pkg/parser/format"
	"github.com/pingcap/tidb/pkg/parser/types"
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
		return []*TableInfo{
			{
				Schema: tableSchema(schema, t.Table),
				Table:  Table(t.Table.Name.String()),
				Action: Create,
			},
		}
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
// INFORMATION_SCHEMA 的 DATA_TYPE/COLUMN_TYPE 语义对齐（如 varchar + "(255)"）。
// 注意：canal 包的 init 会把 ast.NewValueExpr 替换为空实现（裁剪依赖体积），
// 因此 DEFAULT/COMMENT 的表达式值无法从 AST 恢复（restore 恒为空）。这两个字段
// 在实际同步流程中不消费——CREATE/ALTER 会回源库查完整结构，DROP 只用表名，
// 这里保留提取逻辑以兼容未裁剪的解析环境。
func columnToInfo(col *ast.ColumnDef, action string) ColumnInfo {
	c := ColumnInfo{
		Name:   Column(col.Name.Name.String()),
		Action: action,
		IsNull: "YES",
	}
	if col.Tp != nil {
		c.Type, c.Args = splitFieldType(col.Tp)
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
			c.Comment = restoreExpr(opt.Expr)
		}
	}
	return c
}

// splitFieldType 把 AST 字段类型拆成 Type（小写类型名）与 Args（参数部分）。
// CompactStr 与 INFORMATION_SCHEMA.COLUMN_TYPE 对齐（如 "varchar(255)"、
// "decimal(10,2)"、"enum('a','b')"、"bigint(20) unsigned"）；TypeToStr 使用与
// CompactStr 内部相同的 charset，保证 Type 一定是 CompactStr 的前缀。
func splitFieldType(tp *types.FieldType) (string, string) {
	compact := tp.CompactStr()
	typ := types.TypeToStr(tp.GetType(), tp.GetCharset())
	return typ, strings.TrimPrefix(compact, typ)
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
