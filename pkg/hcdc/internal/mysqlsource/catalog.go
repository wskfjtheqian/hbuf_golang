package mysqlsource

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
)

// tableMetadata 读取源表的列与主键元数据，并校验存储引擎和外键规则是否支持同步。
func tableMetadata(ctx context.Context, conn *sql.Conn, ref TableRef, flavor string) (Table, error) {
	table := Table{Ref: ref}
	var engine sql.NullString
	var collation sql.NullString
	err := conn.QueryRowContext(ctx, `SELECT ENGINE, TABLE_COLLATION FROM information_schema.TABLES
		WHERE BINARY TABLE_SCHEMA = BINARY ? AND BINARY TABLE_NAME = BINARY ?`, ref.Schema, ref.Name).Scan(&engine, &collation)
	if err != nil {
		return table, herror.WrapMsg(err, "read source engine for "+qualifiedTable(ref))
	}
	if !engine.Valid || !strings.EqualFold(engine.String, "InnoDB") {
		return table, herror.NewError("source snapshot requires an InnoDB table: " + qualifiedTable(ref))
	}
	table.DefaultCharset, _, _ = strings.Cut(collation.String, "_")
	var cascadingKeys uint64
	err = conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.REFERENTIAL_CONSTRAINTS
		WHERE BINARY CONSTRAINT_SCHEMA = BINARY ? AND BINARY TABLE_NAME = BINARY ?
		AND (UPDATE_RULE IN ('CASCADE', 'SET NULL', 'SET DEFAULT') OR DELETE_RULE IN ('CASCADE', 'SET NULL', 'SET DEFAULT'))`,
		ref.Schema, ref.Name).Scan(&cascadingKeys)
	if err != nil {
		return table, herror.WrapMsg(err, "verify source foreign key rules for "+qualifiedTable(ref))
	}
	if cascadingKeys != 0 {
		return table, herror.NewError("source table has implicit cascading writes that may be absent from the binlog: " + qualifiedTable(ref))
	}
	columns, err := tableColumns(ctx, conn, ref, flavor)
	if err != nil {
		return table, err
	}
	keys, err := primaryKey(ctx, conn, ref, columns)
	if err != nil {
		return table, err
	}
	table.Columns, table.PrimaryKey = columns, keys
	return table, nil
}

// tableColumns 按物理顺序读取源表列，识别默认值、字符集和生成列等属性。
func tableColumns(ctx context.Context, conn *sql.Conn, ref TableRef, flavor string) ([]Column, error) {
	rows, err := conn.QueryContext(ctx, `SELECT COLUMN_NAME, DATA_TYPE, COLUMN_TYPE, IS_NULLABLE, COLUMN_COMMENT,
		COALESCE(CHARACTER_SET_NAME, ''), EXTRA, COLUMN_DEFAULT
		FROM information_schema.COLUMNS WHERE BINARY TABLE_SCHEMA = BINARY ? AND BINARY TABLE_NAME = BINARY ?
		ORDER BY ORDINAL_POSITION`, ref.Schema, ref.Name)
	if err != nil {
		return nil, herror.WrapMsg(err, "read source column metadata")
	}
	defer rows.Close()
	var columns []Column
	for rows.Next() {
		var column Column
		var nullable, extra string
		var defaultValue sql.NullString
		if err := rows.Scan(&column.Name, &column.DataType, &column.ColumnType, &nullable, &column.Comment, &column.Charset, &extra, &defaultValue); err != nil {
			return nil, herror.WrapMsg(err, "decode source column metadata")
		}
		column.Nullable = nullable == "YES"
		extra = strings.ToUpper(extra)
		// MariaDB 可能以不带引号的 SQL 字面量 NULL 返回 DEFAULT NULL。
		noDefault := !defaultValue.Valid || (flavor == mysql.MariaDBFlavor && column.Nullable && defaultValue.String == "NULL")
		column.PlainDefinition = noDefault &&
			extra == "" && !strings.Contains(strings.ToLower(column.ColumnType), " zerofill")
		if strings.Contains(extra, "VIRTUAL GENERATED") || strings.Contains(extra, "STORED GENERATED") || strings.Contains(extra, "PERSISTENT GENERATED") {
			return nil, herror.NewError("source generated column cannot be captured: " + qualifiedTable(ref) + "." + column.Name)
		}
		switch column.Charset {
		case "", "binary", "utf8", "utf8mb3", "utf8mb4", "ascii":
		default:
			return nil, herror.NewError(fmt.Sprintf("unsupported source character set %s for %s.%s", column.Charset, qualifiedTable(ref), column.Name))
		}
		columns = append(columns, column)
	}
	if err := rows.Err(); err != nil {
		return nil, herror.WrapMsg(err, "read source column metadata")
	}
	if len(columns) == 0 {
		return nil, herror.NewError("source table has no columns: " + qualifiedTable(ref))
	}
	return columns, nil
}

// primaryKey 按索引顺序读取完整的主键列，并将其映射为物理列下标。
func primaryKey(ctx context.Context, conn *sql.Conn, ref TableRef, columns []Column) ([]int, error) {
	rows, err := conn.QueryContext(ctx, `SELECT COLUMN_NAME, SUB_PART FROM information_schema.STATISTICS
		WHERE BINARY TABLE_SCHEMA = BINARY ? AND BINARY TABLE_NAME = BINARY ? AND INDEX_NAME = 'PRIMARY'
		ORDER BY SEQ_IN_INDEX`, ref.Schema, ref.Name)
	if err != nil {
		return nil, herror.WrapMsg(err, "read source primary key")
	}
	defer rows.Close()
	columnIndexes := make(map[string]int, len(columns))
	for i, column := range columns {
		columnIndexes[column.Name] = i
	}
	var indexes []int
	for rows.Next() {
		var name sql.NullString
		var prefixLength sql.NullInt64
		if err := rows.Scan(&name, &prefixLength); err != nil {
			return nil, herror.WrapMsg(err, "decode source primary key")
		}
		index, exists := columnIndexes[name.String]
		if !name.Valid || !exists || prefixLength.Valid {
			return nil, herror.NewError("source primary key must consist of complete columns: " + qualifiedTable(ref))
		}
		indexes = append(indexes, index)
	}
	if err := rows.Err(); err != nil {
		return nil, herror.WrapMsg(err, "read source primary key")
	}
	if len(indexes) == 0 {
		return nil, herror.NewError("source table requires a primary key: " + qualifiedTable(ref))
	}
	return indexes, nil
}
