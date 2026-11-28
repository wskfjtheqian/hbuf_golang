package mysqlsource

import (
	"slices"
	"strings"

	"github.com/pingcap/tidb/pkg/parser/ast"
	"github.com/pingcap/tidb/pkg/parser/mysql"
	"github.com/pingcap/tidb/pkg/parser/types"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
)

// planAlterTable 区分兼容加列与需要审批的单列变更，并拒绝不支持的组合操作。
func planAlterTable(before Table, specs []*ast.AlterTableSpec) (*DDLChange, error) {
	for _, spec := range specs {
		switch spec.Tp {
		case ast.AlterTableDropColumn, ast.AlterTableModifyColumn, ast.AlterTableRenameColumn:
			if len(specs) != 1 || spec.IfExists || spec.IfNotExists {
				return nil, herror.NewError("approval-gated ALTER TABLE requires one unconditional column action")
			}
			return planApprovalDDL(before, spec)
		case ast.AlterTableChangeColumn:
			return nil, herror.NewError("selected CHANGE COLUMN is unsupported; use an explicit RENAME COLUMN or widening MODIFY COLUMN")
		case ast.AlterTableRenameTable:
			return nil, herror.NewError("selected ALTER TABLE RENAME changes a configured table identity")
		}
	}
	return planAddedColumns(before, specs)
}

// planApprovalDDL 根据历史结构校验需要审批的删列、改名和整数扩宽操作。
func planApprovalDDL(before Table, spec *ast.AlterTableSpec) (*DDLChange, error) {
	name := spec.OldColumnName
	if spec.Tp == ast.AlterTableModifyColumn {
		if len(spec.NewColumns) != 1 {
			return nil, herror.NewError("selected MODIFY COLUMN requires one column definition")
		}
		name = spec.NewColumns[0].Name
	}
	if name == nil {
		return nil, herror.NewError("approval-gated ALTER TABLE requires an existing column")
	}
	index := columnIndex(before.Columns, name.Name.O)
	if index < 0 {
		return nil, herror.NewError("approval-gated ALTER TABLE column is absent from the historical schema")
	}
	if slices.Contains(before.PrimaryKey, index) {
		return nil, herror.NewError("selected ALTER TABLE changes a primary key column")
	}
	if strings.Contains(before.Columns[index].Comment, "CustomType=Bitmap") {
		return nil, herror.NewError("selected ALTER TABLE changes a Bitmap column")
	}

	after := before
	after.Columns = slices.Clone(before.Columns)
	switch spec.Tp {
	case ast.AlterTableDropColumn:
		after.Columns = slices.Delete(after.Columns, index, index+1)
		after.PrimaryKey = slices.Clone(before.PrimaryKey)
		for i, key := range after.PrimaryKey {
			if key > index {
				after.PrimaryKey[i]--
			}
		}
		return approvalDDL(DDLDropColumn, before, after, "drop a non-key column and remove its stored target values"), nil
	case ast.AlterTableRenameColumn:
		if spec.NewColumnName == nil || columnIndex(before.Columns, spec.NewColumnName.Name.O) >= 0 {
			return nil, herror.NewError("selected RENAME COLUMN requires a distinct unused name")
		}
		after.Columns[index].Name = spec.NewColumnName.Name.O
		return approvalDDL(DDLRenameColumn, before, after, "rename a non-key column while preserving its target values"), nil
	case ast.AlterTableModifyColumn:
		column, err := widenedColumn(before, index, spec)
		if err != nil {
			return nil, err
		}
		after.Columns[index] = column
		return approvalDDL(DDLWidenInteger, before, after, "widen the source integer range with the existing Doris integer type"), nil
	default:
		return nil, herror.NewError("selected ALTER TABLE requires an unsupported DDL barrier")
	}
}

// approvalDDL 封装审批型 DDL 的变更前后结构和审批原因。
func approvalDDL(kind DDLKind, before, after Table, reason string) *DDLChange {
	return &DDLChange{Kind: kind, Before: before, After: after, RequiresApproval: true, Reason: reason}
}

// widenedColumn 验证整数范围扩宽时列名、物理位置、符号、可空性和 Doris 类型保持一致。
func widenedColumn(table Table, index int, spec *ast.AlterTableSpec) (Column, error) {
	old := table.Columns[index]
	definition := spec.NewColumns[0]
	field := definition.Tp
	if field == nil {
		return Column{}, herror.NewError("selected MODIFY COLUMN has no type")
	}
	if definition.Name.Name.O != old.Name {
		return Column{}, herror.NewError("selected MODIFY COLUMN changes column name spelling")
	}
	if !sameColumnPosition(table.Columns, index, spec.Position) {
		return Column{}, herror.NewError("selected MODIFY COLUMN moves the physical column")
	}
	if !old.PlainDefinition || mysql.HasZerofillFlag(field.GetFlag()) ||
		field.GetCharset() != "" || field.GetCollate() != "" {
		return Column{}, herror.NewError("selected MODIFY COLUMN requires a verified definition without default or extra modifiers")
	}
	newType := types.TypeToStr(field.GetType(), field.GetCharset())
	oldWidth, newWidth := integerWidth(old.DataType), integerWidth(newType)
	if oldWidth == 0 || newWidth <= oldWidth {
		return Column{}, herror.NewError("selected MODIFY COLUMN must widen an integer without casting or narrowing")
	}
	unsigned := mysql.HasUnsignedFlag(field.GetFlag())
	if unsigned != strings.Contains(strings.ToLower(old.ColumnType), " unsigned") {
		return Column{}, herror.NewError("selected MODIFY COLUMN changes integer signedness")
	}
	if newType == "bigint" && unsigned {
		return Column{}, herror.NewError("selected MODIFY COLUMN would change the Doris integer type")
	}
	nullable, comment, err := widenedColumnOptions(definition.Options)
	if err != nil {
		return Column{}, err
	}
	if nullable != old.Nullable || comment != old.Comment {
		return Column{}, herror.NewError("selected MODIFY COLUMN changes nullability or comment")
	}
	old.DataType, old.ColumnType = newType, field.InfoSchemaStr()
	return old, nil
}

// integerWidth 返回支持的整数类型位宽，其他类型返回零。
func integerWidth(dataType string) int {
	switch dataType {
	case "tinyint":
		return 8
	case "smallint":
		return 16
	case "mediumint":
		return 24
	case "int", "integer":
		return 32
	case "bigint":
		return 64
	default:
		return 0
	}
}

// sameColumnPosition 判断修改列时指定的物理位置是否与当前位置一致。
func sameColumnPosition(columns []Column, index int, position *ast.ColumnPosition) bool {
	if position == nil || position.Tp == ast.ColumnPositionNone {
		return true
	}
	switch position.Tp {
	case ast.ColumnPositionFirst:
		return index == 0
	case ast.ColumnPositionAfter:
		return index > 0 && position.RelativeColumn != nil &&
			strings.EqualFold(columns[index-1].Name, position.RelativeColumn.Name.O)
	default:
		return false
	}
}

// widenedColumnOptions 提取扩宽操作中的可空性和注释，并拒绝不支持的列选项。
func widenedColumnOptions(options []*ast.ColumnOption) (nullable bool, comment string, err error) {
	nullable = true
	for _, option := range options {
		switch option.Tp {
		case ast.ColumnOptionNull:
			nullable = true
		case ast.ColumnOptionNotNull:
			nullable = false
		case ast.ColumnOptionComment:
			value, ok := option.Expr.(ast.ValueExpr)
			if !ok {
				return false, "", herror.NewError("selected MODIFY COLUMN comment must be a literal")
			}
			comment = value.GetString()
		default:
			return false, "", herror.NewError("selected MODIFY COLUMN changes default or extra column modifiers")
		}
	}
	return nullable, comment, nil
}
