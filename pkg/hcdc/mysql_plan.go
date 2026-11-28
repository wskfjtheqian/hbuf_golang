package cdc

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RoaringBitmap/roaring"
	"github.com/RoaringBitmap/roaring/roaring64"
	"github.com/shopspring/decimal"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc/internal/mysqlsource"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
)

const (
	maxBitmapInputBytes        = 8 << 20
	maxBitmapTextBytes         = 16 << 20
	defaultDorisLoadBatchBytes = 10 << 20
	maxDorisCDCLoadBytes       = 64 << 20
)

type tableLayout struct {
	source         mysqlsource.Table
	target         DorisTable
	fields         []string
	expressions    []string
	bitmapWidths   []int
	zeroDatePolicy ZeroDatePolicy
}

// buildTableLayouts 为所选源表建立 Doris 列类型和行编码布局。
func buildTableLayouts(config PipelineConfig, tables []mysqlsource.Table) ([]tableLayout, error) {
	layouts := make([]tableLayout, 0, len(tables))
	for _, table := range tables {
		layout, err := newTableLayout(config.TargetDatabase, table)
		if err != nil {
			return nil, err
		}
		layout.zeroDatePolicy = config.ZeroDatePolicy
		layouts = append(layouts, layout)
	}
	return layouts, nil
}

// newTableLayout 校验主键与列名，生成目标表结构及 Stream Load 列表达式。
func newTableLayout(database string, table mysqlsource.Table) (tableLayout, error) {
	layout := tableLayout{
		source:       table,
		target:       DorisTable{Database: database, Name: table.Ref.Name},
		bitmapWidths: make([]int, len(table.Columns)),
	}
	if len(table.PrimaryKey) == 0 || !validPipelineIdentifier(table.Ref.Name) {
		return layout, herror.NewError("CDC table layout requires a supported name and a primary key")
	}
	keyColumns := make(map[int]bool, len(table.PrimaryKey))
	for _, index := range table.PrimaryKey {
		if index < 0 || index >= len(table.Columns) || keyColumns[index] {
			return layout, herror.NewError("CDC table primary key contains invalid column positions")
		}
		keyColumns[index] = true
		layout.target.PrimaryKey = append(layout.target.PrimaryKey, table.Columns[index].Name)
	}
	nameSet := make(map[string]bool, len(table.Columns))
	for _, column := range table.Columns {
		name := strings.ToLower(column.Name)
		if !validPipelineIdentifier(column.Name) || nameSet[name] || name == "__cdc_delete" || name == "__op" {
			return layout, herror.NewError("CDC column name conflicts with a target column or system field")
		}
		nameSet[name] = true
	}
	columns := make([]DorisColumn, len(table.Columns))
	for index, column := range table.Columns {
		typ, bitmap, err := mysqlColumnType(column, keyColumns[index])
		if err != nil {
			return layout, herror.WrapMsg(err, "map source column "+table.Ref.Schema+"."+table.Ref.Name+"."+column.Name)
		}
		// Doris 要求 BITMAP 列为 NOT NULL，源数据的空值在编码时检查。
		columns[index] = DorisColumn{Name: column.Name, Type: typ, Nullable: column.Nullable && bitmap == 0}
		layout.bitmapWidths[index] = bitmap
		field := "__cdc_value_" + strconv.Itoa(index)
		for nameSet[field] {
			field = "_" + field
		}
		layout.fields = append(layout.fields, field)
		expression := "if(" + field + "='-', '', from_base64(" + field + "))"
		if bitmap != 0 {
			expression = "if(" + field + "='-', bitmap_empty(), bitmap_from_string(from_base64(" + field + ")))"
		} else if isMySQLBinaryColumn(column) {
			// 在 Doris 3.x 中以可逆 Base64 保存原始字节，并保留 SQL NULL 标记。
			expression = "if(" + field + "='-', '', " + field + ")"
		} else if column.DataType == "json" {
			expression = "cast(from_base64(" + field + ") as json)"
		}
		layout.expressions = append(layout.expressions, "`"+column.Name+"`="+expression)
	}
	for _, index := range table.PrimaryKey {
		layout.target.Columns = append(layout.target.Columns, columns[index])
	}
	for index, column := range columns {
		if !keyColumns[index] {
			layout.target.Columns = append(layout.target.Columns, column)
		}
	}
	return layout, nil
}

// mysqlColumnType 将源列映射为 Doris 类型，并返回自定义位图的位宽。
func mysqlColumnType(column mysqlsource.Column, isKey bool) (string, int, error) {
	var markers []string
	for _, token := range strings.FieldsFunc(column.Comment, func(char rune) bool {
		return !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' ||
			char >= '0' && char <= '9' || char == '_' || char == '=')
	}) {
		if strings.HasPrefix(token, "CustomType=Bitmap") {
			markers = append(markers, token)
		}
	}
	if len(markers) != 0 {
		if len(markers) != 1 || !isMySQLBinaryColumn(column) || column.DataType == "tinyblob" || isKey {
			return "", 0, herror.NewError("CDC Bitmap requires exactly one known marker on a supported non-key binary column")
		}
		switch markers[0] {
		case "CustomType=Bitmap32":
			return "BITMAP", 32, nil
		case "CustomType=Bitmap64":
			return "BITMAP", 64, nil
		default:
			return "", 0, herror.NewError("CDC Bitmap has an unknown format marker")
		}
	}
	switch column.DataType {
	case "tinyint", "smallint", "mediumint", "int", "integer", "year":
		return "BIGINT", 0, nil
	case "bigint":
		if strings.Contains(column.ColumnType, "unsigned") {
			return "LARGEINT", 0, nil
		}
		return "BIGINT", 0, nil
	case "varchar", "char":
		if !isKey {
			return "STRING", 0, nil
		}
		if length := mysqlColumnLength(column); length > 0 && length <= 16383 {
			return fmt.Sprintf("VARCHAR(%d)", length*4), 0, nil
		}
	case "binary", "varbinary", "tinyblob", "blob", "mediumblob", "longblob":
		if !isKey {
			return "STRING", 0, nil
		}
		if length := mysqlColumnLength(column); length > 0 && length <= 49149 {
			return fmt.Sprintf("VARCHAR(%d)", base64.StdEncoding.EncodedLen(length)), 0, nil
		}
	case "tinytext", "text", "mediumtext", "longtext":
		if !isKey {
			return "STRING", 0, nil
		}
	case "decimal", "numeric":
		_, args, _ := strings.Cut(column.ColumnType, "(")
		args, _, _ = strings.Cut(args, ")")
		p, s, ok := strings.Cut(args, ",")
		precision, pErr := strconv.Atoi(p)
		scale, sErr := strconv.Atoi(s)
		if ok && pErr == nil && sErr == nil && precision >= 1 && precision <= 38 && scale >= 0 && scale <= precision {
			return fmt.Sprintf("DECIMAL(%d,%d)", precision, scale), 0, nil
		}
	case "date":
		return "DATE", 0, nil
	case "datetime", "timestamp":
		return "DATETIME(6)", 0, nil
	case "time":
		// MySQL TIME 表示有符号时长，绝对值最大可达 838:59:59。
		return "VARCHAR(17)", 0, nil
	case "json":
		if !isKey {
			return "JSON", 0, nil
		}
	case "float", "double", "real":
		if !isKey {
			return "DOUBLE", 0, nil
		}
	}
	return "", 0, herror.NewError("unsupported CDC source column type: " + column.ColumnType)
}

// mysqlColumnLength 从源列类型声明中解析长度参数。
func mysqlColumnLength(column mysqlsource.Column) int {
	_, arguments, _ := strings.Cut(column.ColumnType, "(")
	length, _, _ := strings.Cut(arguments, ")")
	value, _ := strconv.Atoi(length)
	return value
}

// isMySQLBinaryColumn 判断源列是否属于二进制字符串或 BLOB 类型。
func isMySQLBinaryColumn(column mysqlsource.Column) bool {
	switch column.DataType {
	case "binary", "varbinary", "tinyblob", "blob", "mediumblob", "longblob":
		return true
	default:
		return false
	}
}

// encodeRow 按源物理列序编码一行，返回删除标记及本行是否映射了零日期。
func (l tableLayout) encodeRow(values []any, deleted bool) (DorisRow, bool, error) {
	if len(values) != len(l.source.Columns) {
		return DorisRow{}, false, herror.NewError("CDC source row does not match the physical column layout")
	}
	row := DorisRow{Values: make([]*string, len(values)), Delete: deleted}
	var zeroDateMapped bool
	for index, value := range values {
		column := l.source.Columns[index]
		if value == nil {
			if l.bitmapWidths[index] != 0 {
				return DorisRow{}, false, herror.NewError(fmt.Sprintf("CDC source Bitmap column %s.%s.%s contains SQL NULL", l.source.Ref.Schema, l.source.Ref.Name, column.Name))
			}
			if !column.Nullable {
				return DorisRow{}, false, herror.NewError("CDC source contains NULL for required column " + column.Name)
			}
			continue
		}
		var encoded string
		var err error
		if l.bitmapWidths[index] != 0 {
			encoded, err = bitmapMembers(value, l.bitmapWidths[index])
		} else if isMySQLBinaryColumn(column) {
			encoded, err = sourceBinaryText(value)
		} else {
			encoded, err = sourceValueText(column, value)
		}
		if err != nil {
			return DorisRow{}, false, herror.WrapMsg(err, "encode source column "+column.Name)
		}
		switch column.DataType {
		case "date", "datetime", "timestamp":
			const zeroDate, epochDate = "0000-00-00", "1970-01-01"
			if suffix, zero := strings.CutPrefix(encoded, zeroDate); zero {
				if l.zeroDatePolicy != ZeroDateEpoch {
					return DorisRow{}, false, herror.NewError(fmt.Sprintf("CDC source column %s.%s.%s contains a zero date", l.source.Ref.Schema, l.source.Ref.Name, column.Name))
				}
				encoded = epochDate + suffix
				zeroDateMapped = true
			} else if l.zeroDatePolicy == ZeroDateEpoch && strings.HasPrefix(encoded, epochDate) && slices.Contains(l.source.PrimaryKey, index) {
				return DorisRow{}, false, herror.NewError(fmt.Sprintf("CDC source key column %s.%s.%s uses the date reserved for zero date mapping", l.source.Ref.Schema, l.source.Ref.Name, column.Name))
			}
		}
		field := "-"
		if encoded != "" {
			field = base64.StdEncoding.EncodeToString([]byte(encoded))
		}
		row.Values[index] = &field
	}
	return row, zeroDateMapped, nil
}

// encodedDorisRowSize 计算一行的 CSV 字节数，包含分隔符、删除标记和换行。
// 列表达式独立于每行大小。
func encodedDorisRowSize(row DorisRow) int {
	size := len(row.Values) + 2
	for _, value := range row.Values {
		if value == nil {
			size += len(`\N`)
		} else {
			size += len(*value)
		}
	}
	return size
}

// checkLoadRowSize 检查经过编码的单行是否超过 Doris 导入硬上限。
func (l tableLayout) checkLoadRowSize(size int) error {
	if size > maxDorisCDCLoadBytes {
		return herror.NewError(fmt.Sprintf("encoded source row for %s.%s has %d bytes, limit %d",
			l.source.Ref.Schema, l.source.Ref.Name, size, maxDorisCDCLoadBytes))
	}
	return nil
}

// rowKey 从已编码的主键字段构造事务内归并键。
func (l tableLayout) rowKey(row DorisRow) (string, error) {
	var key strings.Builder
	for _, index := range l.source.PrimaryKey {
		value := row.Values[index]
		if value == nil {
			return "", herror.NewError("CDC transaction contains NULL primary key")
		}
		key.WriteString(*value)
		key.WriteByte(':')
	}
	return key.String(), nil
}

// batch 将一组已编码行及其列映射打包为目标表导入批次。
func (l tableLayout) batch(rows []DorisRow) (LoadBatch, error) {
	payload, err := EncodeDorisLoadWithExpressions(l.fields, l.expressions, rows)
	if err != nil {
		return LoadBatch{}, err
	}
	return LoadBatch{
		TargetTable: l.target.Database + "." + l.target.Name, RowCount: uint64(len(rows)), Payload: payload,
	}, nil
}

// sourceBinaryText 将快照或 binlog 返回的二进制值转换为可逆 Base64 文本。
func sourceBinaryText(value any) (string, error) {
	switch value := value.(type) {
	case []byte:
		return string(value), nil
	case string:
		return value, nil
	default:
		return "", herror.NewError(fmt.Sprintf("unsupported CDC binary value type %T", value))
	}
}

// sourceValueText 将源列值转换为 Doris 可解析的文本，保留数值和时间语义。
func sourceValueText(column mysqlsource.Column, value any) (string, error) {
	var text string
	switch v := value.(type) {
	case string:
		text = v
	case []byte:
		text = string(v)
	case json.RawMessage:
		text = string(v)
	case int8, int16, int32, int64, int, uint8, uint16, uint32, uint64, uint, float32, float64:
		return fmt.Sprint(v), nil
	case decimal.Decimal:
		return v.String(), nil
	case time.Time:
		return v.UTC().Format("2006-01-02 15:04:05.999999"), nil
	default:
		return "", herror.NewError(fmt.Sprintf("unsupported CDC source value type %T", value))
	}
	if !utf8.ValidString(text) {
		return "", herror.NewError("CDC text contains invalid UTF-8")
	}
	switch column.DataType {
	case "json":
		if !json.Valid([]byte(text)) {
			return "", herror.NewError("CDC source JSON contains an invalid document")
		}
	case "time", "datetime", "timestamp":
		// 快照驱动按声明的精度保留小数位，binlog 解码器可能省略全零的小数部分。
		if whole, fraction, ok := strings.Cut(text, "."); ok {
			fraction = strings.TrimRight(fraction, "0")
			text = whole
			if fraction != "" {
				text += "." + fraction
			}
		}
	}
	return text, nil
}

// bitmapMembers 校验并解码 Bitmap32 或 Bitmap64，生成 Doris 可识别的成员文本。
func bitmapMembers(value any, width int) (_ string, resultErr error) {
	var binaryValue []byte
	switch v := value.(type) {
	case []byte:
		binaryValue = v
	case string:
		binaryValue = []byte(v)
	default:
		return "", herror.NewError("CDC Bitmap requires a binary source value")
	}
	if len(binaryValue) < 8 || len(binaryValue) > maxBitmapInputBytes {
		return "", herror.NewError("CDC Bitmap source value has invalid size")
	}
	// 可移植解码器可能接受迭代器无法遍历的坏容器。在输入边界将其转换为错误。
	defer func() {
		if recover() != nil {
			resultErr = herror.NewError("CDC Bitmap contains an invalid Roaring container")
		}
	}()
	reader := bytes.NewReader(binaryValue)
	var next func() (uint64, bool)
	var cardinality uint64
	if width == 32 {
		bitmap := roaring.NewBitmap()
		_, err := bitmap.ReadFrom(reader)
		if err != nil || reader.Len() != 0 {
			return "", herror.NewError("CDC Bitmap32 payload does not contain one complete Roaring bitmap")
		}
		cardinality = bitmap.GetCardinality()
		it := bitmap.Iterator()
		next = func() (uint64, bool) {
			if !it.HasNext() {
				return 0, false
			}
			return uint64(it.Next()), true
		}
	} else {
		// 每个条目包含四字节键和至少八字节的嵌套位图。
		if binary.LittleEndian.Uint64(binaryValue) > uint64((len(binaryValue)-8)/12) {
			return "", herror.NewError("CDC Bitmap64 payload declares more containers than its length permits")
		}
		bitmap := roaring64.NewBitmap()
		// 使用读取器报告的实际剩余字节，避免受 Roaring 的嵌套 32 位容器计数影响。
		_, err := bitmap.ReadFrom(reader)
		if err != nil || reader.Len() != 0 {
			return "", herror.NewError("CDC Bitmap64 payload does not contain one complete Roaring bitmap")
		}
		cardinality = bitmap.GetCardinality()
		it := bitmap.Iterator()
		next = func() (uint64, bool) {
			if !it.HasNext() {
				return 0, false
			}
			return it.Next(), true
		}
	}
	var result strings.Builder
	var seen, previous uint64
	var digits [20]byte
	for member, ok := next(); ok; member, ok = next() {
		if seen != 0 {
			if member <= previous {
				return "", herror.NewError("CDC Bitmap contains unordered or duplicate Roaring members")
			}
			result.WriteByte(',')
		}
		result.Write(strconv.AppendUint(digits[:0], member, 10))
		if result.Len() > maxBitmapTextBytes {
			return "", herror.NewError("CDC Bitmap expanded member list exceeds its byte limit")
		}
		seen++
		previous = member
	}
	if seen != cardinality {
		return "", herror.NewError("CDC Bitmap member count differs from its Roaring cardinality")
	}
	return result.String(), nil
}
