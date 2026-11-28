package mysqlsource

import (
	"slices"
	"strings"

	"github.com/pingcap/tidb/pkg/parser/ast"
	"github.com/pingcap/tidb/pkg/parser/mysql"
	"github.com/pingcap/tidb/pkg/parser/types"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
)

// DDLKind 标识源端 DDL 的目标处理方式。
type DDLKind string

const (
	// DDLAddColumns 表示订阅表上可兼容的加列操作。
	DDLAddColumns DDLKind = "add_columns"
	// DDLTruncate 清空订阅表并保留原列结构。
	DDLTruncate DDLKind = "truncate"
	// DDLDropColumn 删除单个非主键列。
	DDLDropColumn DDLKind = "drop_column"
	// DDLWidenInteger 扩宽源端整数取值范围，目标 Doris 类型保持原样。
	DDLWidenInteger DDLKind = "widen_integer"
	// DDLRenameColumn 就地重命名单个非主键列。
	DDLRenameColumn DDLKind = "rename_column"
	// DDLUnsupported 表示需要人工处理的 DDL 屏障。
	DDLUnsupported DDLKind = "unsupported"
)

// DDLChange 保存已提交 QueryEvent 变更前后的表结构。
// 消费方持久化屏障和下一版结构后才返回成功，源端随后使用新结构解码。
type DDLChange struct {
	Kind             DDLKind
	Before           Table
	After            Table
	AddedColumns     []Column
	RequiresApproval bool
	Reason           string
}

// planDDL 根据已订阅表的历史结构为源端 DDL 生成变更或阻塞计划。
func (r *transactionReader) planDDL(schema string, statement ast.StmtNode) *DDLChange {
	switch event := statement.(type) {
	case *ast.AlterTableStmt:
		for _, spec := range event.Specs {
			if spec.NewTable != nil {
				if blocked := r.blockedTables(schema, "selected ALTER TABLE references a configured destination", spec.NewTable); blocked != nil {
					return blocked
				}
			}
		}
		before, selected := r.tables[ddlTableRef(schema, event.Table)]
		if !selected {
			return r.blockedTables(schema, "selected ALTER TABLE uses a different identifier spelling", event.Table)
		}
		change, err := planAlterTable(before, event.Specs)
		if err != nil {
			return unsupportedDDL(before, err.Error())
		}
		return change
	case *ast.TruncateTableStmt:
		before, selected := r.tables[ddlTableRef(schema, event.Table)]
		if !selected {
			return r.blockedTables(schema, "selected TRUNCATE TABLE uses a different identifier spelling", event.Table)
		}
		return &DDLChange{Kind: DDLTruncate, Before: before, After: before, RequiresApproval: true}
	case *ast.CreateTableStmt:
		return r.blockedTables(schema, "selected CREATE TABLE replaces a configured table identity", event.Table)
	case *ast.DropTableStmt:
		return r.blockedTables(schema, "selected DROP TABLE requires table removal recovery", event.Tables...)
	case *ast.RenameTableStmt:
		for _, pair := range event.TableToTables {
			if blocked := r.blockedTables(schema, "selected RENAME TABLE changes a configured table identity", pair.OldTable, pair.NewTable); blocked != nil {
				return blocked
			}
		}
		return nil
	case *ast.CreateIndexStmt:
		return r.blockedTables(schema, "selected CREATE INDEX requires a manual schema decision", event.Table)
	case *ast.DropIndexStmt:
		return r.blockedTables(schema, "selected DROP INDEX requires a manual schema decision", event.Table)
	case *ast.DropDatabaseStmt:
		return r.blockedSchema(event.Name.O, "selected DROP DATABASE requires database removal recovery")
	case *ast.CreateDatabaseStmt:
		return r.blockedSchema(event.Name.O, "selected CREATE DATABASE requires a manual schema decision")
	case *ast.AlterDatabaseStmt:
		name := event.Name.O
		if name == "" {
			name = schema
		}
		return r.blockedSchema(name, "selected ALTER DATABASE requires a manual schema decision")
	default:
		return nil
	}
}

// ddlTableRef 补齐 DDL 语句中省略的库名，并保留原始库表名称。
func ddlTableRef(schema string, name *ast.TableName) TableRef {
	ref := TableRef{Schema: name.Schema.O, Name: name.Name.O}
	if ref.Schema == "" {
		ref.Schema = schema
	}
	return ref
}

// blockedTables 在指定表名命中订阅范围时生成阻塞计划，也检查大小写不同的写法。
func (r *transactionReader) blockedTables(schema, reason string, names ...*ast.TableName) *DDLChange {
	for _, name := range names {
		ref := ddlTableRef(schema, name)
		if table, selected := r.tables[ref]; selected {
			return unsupportedDDL(table, reason)
		}
		if ref.Schema == "" {
			continue
		}
		if blocked := r.firstBlockedTable(reason, func(candidate TableRef) bool {
			return strings.EqualFold(candidate.Schema, ref.Schema) && strings.EqualFold(candidate.Name, ref.Name)
		}); blocked != nil {
			return blocked
		}
	}
	return nil
}

// blockedSchema 为命中指定数据库的第一张订阅表生成阻塞计划。
func (r *transactionReader) blockedSchema(schema, reason string) *DDLChange {
	if schema == "" {
		return nil
	}
	return r.firstBlockedTable(reason, func(ref TableRef) bool {
		return strings.EqualFold(ref.Schema, schema)
	})
}

// firstBlockedTable 按库名和表名顺序选择第一个满足条件的表，令阻塞结果保持确定。
func (r *transactionReader) firstBlockedTable(reason string, match func(TableRef) bool) *DDLChange {
	var found *Table
	for ref, table := range r.tables {
		if match(ref) && (found == nil || ref.Schema < found.Ref.Schema ||
			(ref.Schema == found.Ref.Schema && ref.Name < found.Ref.Name)) {
			found = &table
		}
	}
	if found == nil {
		return nil
	}
	return unsupportedDDL(*found, reason)
}

// unsupportedDDL 用原结构和失败原因构建无法自动推进的 DDL 屏障。
func unsupportedDDL(before Table, reason string) *DDLChange {
	return &DDLChange{Kind: DDLUnsupported, Before: before, After: before, RequiresApproval: true, Reason: reason}
}

// planAddedColumns 计算兼容加列后的物理结构和主键列下标。
func planAddedColumns(before Table, specs []*ast.AlterTableSpec) (*DDLChange, error) {
	after := before
	after.Columns = slices.Clone(before.Columns)
	after.PrimaryKey = slices.Clone(before.PrimaryKey)
	var added []Column
	for _, spec := range specs {
		switch spec.Tp {
		case ast.AlterTableAlgorithm, ast.AlterTableLock:
			continue
		case ast.AlterTableAddColumns:
			if len(spec.NewColumns) == 0 || len(spec.NewConstraints) != 0 {
				return nil, herror.NewError("selected ALTER TABLE changes unsupported constraints")
			}
		default:
			return nil, herror.NewError("selected ALTER TABLE requires an unsupported DDL barrier")
		}
		for _, definition := range spec.NewColumns {
			if columnIndex(after.Columns, definition.Name.Name.O) >= 0 {
				if spec.IfNotExists {
					continue
				}
				return nil, herror.NewError("selected ADD COLUMN conflicts with historical column " + definition.Name.Name.O)
			}
			column, err := compatibleAddedColumn(definition, before.DefaultCharset)
			if err != nil {
				return nil, err
			}
			index, err := addedColumnIndex(after.Columns, spec.Position)
			if err != nil {
				return nil, err
			}
			after.Columns = slices.Insert(after.Columns, index, column)
			for i, keyIndex := range after.PrimaryKey {
				if keyIndex >= index {
					after.PrimaryKey[i]++
				}
			}
			added = append(added, column)
		}
	}
	return &DDLChange{Kind: DDLAddColumns, Before: before, After: after, AddedColumns: added}, nil
}

// addedColumnIndex 根据 FIRST 或 AFTER 子句解析新增列的物理位置。
func addedColumnIndex(columns []Column, position *ast.ColumnPosition) (int, error) {
	if position == nil || position.Tp == ast.ColumnPositionNone {
		return len(columns), nil
	}
	switch position.Tp {
	case ast.ColumnPositionFirst:
		return 0, nil
	case ast.ColumnPositionAfter:
		if position.RelativeColumn != nil {
			index := columnIndex(columns, position.RelativeColumn.Name.O)
			if index >= 0 {
				return index + 1, nil
			}
		}
	}
	return 0, herror.NewError("selected ADD COLUMN refers to an unknown physical column position")
}

// columnIndex 查找名称匹配的物理列并忽略大小写，未找到时返回 -1。
func columnIndex(columns []Column, name string) int {
	return slices.IndexFunc(columns, func(column Column) bool {
		return strings.EqualFold(column.Name, name)
	})
}

// compatibleAddedColumn 校验新增列的类型、字符集及约束，并构建可自动应用的列定义。
func compatibleAddedColumn(definition *ast.ColumnDef, defaultCharset string) (Column, error) {
	name := definition.Name.Name.O
	field := definition.Tp
	if field == nil {
		return Column{}, herror.NewError("selected ADD COLUMN has no type for " + name)
	}
	column := Column{
		Name:            name,
		DataType:        types.TypeToStr(field.GetType(), field.GetCharset()),
		ColumnType:      field.InfoSchemaStr(),
		Charset:         field.GetCharset(),
		Nullable:        true,
		PlainDefinition: !mysql.HasZerofillFlag(field.GetFlag()),
	}
	if column.Charset == "" {
		switch column.DataType {
		case "char", "varchar", "tinytext", "text", "mediumtext", "longtext":
			if defaultCharset == "" {
				return Column{}, herror.NewError("historical table charset is missing for selected ADD COLUMN " + name)
			}
			column.Charset = defaultCharset
		}
	}
	switch column.Charset {
	case "", "binary", "utf8", "utf8mb3", "utf8mb4", "ascii":
	default:
		return Column{}, herror.NewError("selected ADD COLUMN uses an unsupported character set on " + name)
	}
	var explicitNull bool
	for _, option := range definition.Options {
		switch option.Tp {
		case ast.ColumnOptionNull:
			explicitNull = true
		case ast.ColumnOptionDefaultValue:
			value, ok := option.Expr.(ast.ValueExpr)
			if !ok || value.GetValue() != nil {
				return Column{}, herror.NewError("selected ADD COLUMN has a non-null default value on " + name)
			}
		case ast.ColumnOptionComment:
			value, ok := option.Expr.(ast.ValueExpr)
			if !ok {
				return Column{}, herror.NewError("selected ADD COLUMN comment is not a literal on " + name)
			}
			column.Comment = value.GetString()
		default:
			return Column{}, herror.NewError("selected ADD COLUMN has an unsupported constraint on " + name)
		}
	}
	if column.DataType == "timestamp" && !explicitNull {
		return Column{}, herror.NewError("selected ADD COLUMN TIMESTAMP must explicitly allow NULL on " + name)
	}
	return column, nil
}
