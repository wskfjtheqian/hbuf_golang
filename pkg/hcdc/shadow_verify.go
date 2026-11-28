package hcdc

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"hash"
	"math/big"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
)

// ShadowTable 指定影子迁移数据核对的一方及其表。
type ShadowTable struct {
	DB     *sql.DB `yaml:"-"`
	Schema string  `yaml:"Schema"`
	Table  string  `yaml:"Table"`
}

// ShadowKeyRange 按配置的复合主键选取 [Start, End) 区间，空边界表示该端无限制。
// 区间采用键的 SQL 自然顺序，边界值使用 SQL 标量。
// 切换核对要求相邻区间的衔接边界具有相同的值和类型。
type ShadowKeyRange struct {
	Start []any `yaml:"Start"`
	End   []any `yaml:"End"`
}

// ShadowVerificationConfig 为三个数据库指定相同的列投影和键顺序。
// Columns 必须包含完整主键。SQL 数值键按数值排序，其他键采用相容的二进制顺序。
// 各连接的 SQL 驱动应使用相同的文本转换选项。
type ShadowVerificationConfig struct {
	Source      ShadowTable      `yaml:"Source"`
	Legacy      ShadowTable      `yaml:"Legacy"`
	Shadow      ShadowTable      `yaml:"Shadow"`
	Columns     []string         `yaml:"Columns"`
	KeyColumns  []string         `yaml:"KeyColumns"`
	Ranges      []ShadowKeyRange `yaml:"Ranges"`
	SampleLimit int              `yaml:"SampleLimit"`
}

// ShadowDigest 记录单个键区间内的行数及按行顺序计算的 SHA-256 摘要。
type ShadowDigest struct {
	Rows   uint64
	SHA256 string
}

// ShadowValue 保存数据库字段的文本表示，Valid 为假时表示 SQL NULL。
type ShadowValue struct {
	Text  string
	Valid bool
}

// ShadowRow 按配置的列顺序保存参与核对的投影值。
type ShadowRow struct {
	Values []ShadowValue
}

// ShadowDifference 记录单个键在各方的不同数据，该方对应的行指针为 nil 表示缺失该键。
type ShadowDifference struct {
	RangeIndex int
	Key        []string
	Source     *ShadowRow
	Legacy     *ShadowRow
	Shadow     *ShadowRow
}

// ShadowRangeVerification 汇总三张表在同一主键区间内的核对结果。
type ShadowRangeVerification struct {
	Range       ShadowKeyRange
	Source      ShadowDigest
	Legacy      ShadowDigest
	Shadow      ShadowDigest
	Differences uint64
	Equal       bool
}

// ShadowVerification 保存各区间的摘要及限定数量的差异样例。
type ShadowVerification struct {
	Equal       bool
	Ranges      []ShadowRangeVerification
	Differences []ShadowDifference
}

// VerifyShadow 在影子库切换或回退前核对已存储的数据。
// 核对期间，调用方必须冻结源写入，或通过其他方式使各方的数据对应同一个源位点。
// 每个核对区间都会为每一方打开一条 SQL 结果流。
// 共用受限连接池时，池中须有足够的并发连接提供这些结果流。
// 数据存在差异时返回不一致的核对结果。
// 输入无效、数据库出错或排序规则不兼容时返回错误。
func VerifyShadow(ctx context.Context, config ShadowVerificationConfig) (ShadowVerification, error) {
	return verifyShadow(ctx, config, false, nil)
}

// verifyShadow 校验核对配置，按指定键区间比较并汇总结果和差异样例。
func verifyShadow(ctx context.Context, config ShadowVerificationConfig, requireFullCoverage bool, sourceMember *Cursor) (ShadowVerification, error) {
	keys, err := validateShadowVerification(config)
	if err != nil {
		return ShadowVerification{}, err
	}
	ranges := config.Ranges
	if len(ranges) == 0 {
		ranges = []ShadowKeyRange{{}}
	}
	if requireFullCoverage {
		if err := validateShadowRangeCoverage(ranges); err != nil {
			return ShadowVerification{}, err
		}
	}
	result := ShadowVerification{Equal: true}
	for index, keyRange := range ranges {
		verified, differences, err := verifyShadowRange(ctx, config, keys, keyRange, index, config.SampleLimit-len(result.Differences), sourceMember)
		if err != nil {
			return ShadowVerification{}, herror.WrapMsg(err, fmt.Sprintf("verify shadow key range %d", index))
		}
		result.Ranges = append(result.Ranges, verified)
		result.Differences = append(result.Differences, differences...)
		result.Equal = result.Equal && verified.Equal
	}
	return result, nil
}

// validateShadowRangeCoverage 检查各区间连续衔接并覆盖整个主键范围。
func validateShadowRangeCoverage(ranges []ShadowKeyRange) error {
	if len(ranges[0].Start) != 0 || len(ranges[len(ranges)-1].End) != 0 {
		return herror.NewError("cutover comparison must cover the full primary-key range")
	}
	for index := 1; index < len(ranges); index++ {
		boundary := ranges[index].Start
		if len(boundary) == 0 || !reflect.DeepEqual(ranges[index-1].End, boundary) {
			return herror.NewError("cutover comparison ranges must share adjacent bounds without gaps or overlap")
		}
	}
	return nil
}

// validateShadowVerification 校验连接池容量、表名、列投影及范围边界，返回主键在投影中的位置。
func validateShadowVerification(config ShadowVerificationConfig) ([]int, error) {
	if config.SampleLimit < 0 {
		return nil, herror.NewError("shadow comparison requires a nonnegative sample limit")
	}
	poolStreams := make(map[*sql.DB]int)
	for _, table := range []ShadowTable{config.Source, config.Legacy, config.Shadow} {
		if table.DB == nil || !dorisIdentifier.MatchString(table.Schema) || !dorisIdentifier.MatchString(table.Table) {
			return nil, herror.NewError("shadow comparison requires a valid database pool, schema and table name")
		}
		poolStreams[table.DB]++
		if limit := table.DB.Stats().MaxOpenConnections; limit > 0 && poolStreams[table.DB] > limit {
			return nil, herror.NewError("shadow comparison needs one available SQL connection per participant")
		}
	}
	keys, err := shadowKeyIndices(config.Columns, config.KeyColumns)
	if err != nil {
		return nil, err
	}
	for _, keyRange := range config.Ranges {
		for _, bound := range [][]any{keyRange.Start, keyRange.End} {
			if len(bound) != 0 && len(bound) != len(keys) {
				return nil, herror.NewError("shadow comparison range bounds must contain the full composite key")
			}
			for _, value := range bound {
				if _, err := shadowBoundText(value); err != nil {
					return nil, err
				}
			}
		}
	}
	return keys, nil
}

// shadowKeyIndices 校验列投影和复合主键，返回各主键列在投影中的索引。
func shadowKeyIndices(projectedColumns, keyColumns []string) ([]int, error) {
	if len(projectedColumns) == 0 || len(keyColumns) == 0 {
		return nil, herror.NewError("shadow comparison requires projected columns and key columns")
	}
	columns := make(map[string]int, len(projectedColumns))
	for index, column := range projectedColumns {
		name := strings.ToLower(column)
		if _, exists := columns[name]; exists || !dorisIdentifier.MatchString(column) {
			return nil, herror.NewError("shadow comparison column names must be valid and unique")
		}
		columns[name] = index
	}
	keys := make([]int, 0, len(keyColumns))
	for _, key := range keyColumns {
		index, ok := columns[strings.ToLower(key)]
		if !ok || slices.Contains(keys, index) || !dorisIdentifier.MatchString(key) {
			return nil, herror.NewError("shadow comparison key must contain distinct projected columns")
		}
		keys = append(keys, index)
	}
	return keys, nil
}

// shadowRangeQuery 生成按配置主键排序的范围查询及复合边界绑定参数。
func shadowRangeQuery(table ShadowTable, config ShadowVerificationConfig, keyRange ShadowKeyRange) (string, []any) {
	identifiers := func(columns []string) string {
		quoted := make([]string, len(columns))
		for i, column := range columns {
			quoted[i] = quoteDoris(column)
		}
		return strings.Join(quoted, ", ")
	}
	var predicates []string
	var args []any
	addBound := func(values []any, direction string, inclusive bool) {
		if len(values) == 0 {
			return
		}
		comparisons := make([]string, 0, len(values))
		for columnIndex := range values {
			parts := make([]string, 0, columnIndex+1)
			for prefix := 0; prefix < columnIndex; prefix++ {
				parts = append(parts, quoteDoris(config.KeyColumns[prefix])+" = ?")
				args = append(args, values[prefix])
			}
			operator := direction
			if inclusive && columnIndex == len(values)-1 {
				operator += "="
			}
			parts = append(parts, quoteDoris(config.KeyColumns[columnIndex])+" "+operator+" ?")
			args = append(args, values[columnIndex])
			comparisons = append(comparisons, "("+strings.Join(parts, " AND ")+")")
		}
		predicates = append(predicates, "("+strings.Join(comparisons, " OR ")+")")
	}
	addBound(keyRange.Start, ">", true)
	addBound(keyRange.End, "<", false)

	query := "SELECT " + identifiers(config.Columns) + " FROM " + quoteDoris(table.Schema) + "." + quoteDoris(table.Table)
	if len(predicates) != 0 {
		query += " WHERE " + strings.Join(predicates, " AND ")
	}
	return query + " ORDER BY " + identifiers(config.KeyColumns), args
}

type shadowKeyPart struct {
	text    string
	numeric *big.Rat
}

type shadowStream struct {
	label string
	rows  *sql.Rows
	hash  hash.Hash
	count uint64
	key   []shadowKeyPart
	row   *ShadowRow
}

// verifyShadowRange 流式归并比较三方指定范围内的行，并计算各方摘要。
func verifyShadowRange(ctx context.Context, config ShadowVerificationConfig, keys []int, keyRange ShadowKeyRange, index, sampleLimit int, sourceMember *Cursor) (ShadowRangeVerification, []ShadowDifference, error) {
	result := ShadowRangeVerification{Range: keyRange, Equal: true}
	participants := []struct {
		label string
		table ShadowTable
	}{
		{"source", config.Source}, {"legacy", config.Legacy}, {"shadow", config.Shadow},
	}
	var streams [3]shadowStream
	for i, participant := range participants {
		var queryer interface {
			QueryContext(context.Context, string, ...any) (*sql.Rows, error)
		} = participant.table.DB
		if i == 0 && sourceMember != nil {
			conn, err := participant.table.DB.Conn(ctx)
			if err != nil {
				return result, nil, herror.WrapMsg(err, "obtain source connection for shadow comparison")
			}
			defer conn.Close()
			if err := verifyShadowSourceMember(ctx, conn, *sourceMember); err != nil {
				return result, nil, err
			}
			queryer = conn
		}
		query, args := shadowRangeQuery(participant.table, config, keyRange)
		rows, err := queryer.QueryContext(ctx, query, args...)
		if err != nil {
			return result, nil, herror.WrapMsg(err, "read "+participant.label+" shadow comparison rows")
		}
		defer rows.Close()
		streams[i] = shadowStream{label: participant.label, rows: rows, hash: sha256.New()}
	}

	sourceTypes, err := streams[0].rows.ColumnTypes()
	if err != nil {
		return result, nil, herror.WrapMsg(err, "read source key types")
	}
	if len(sourceTypes) != len(config.Columns) {
		return result, nil, herror.NewError("source shadow comparison projection does not match the configured columns")
	}
	numericKeys := make([]bool, len(keys))
	for i, key := range keys {
		numericKeys[i] = shadowNumericKey(sourceTypes[key].DatabaseTypeName())
	}
	if err := validateShadowRangeOrder(keyRange, numericKeys); err != nil {
		return result, nil, err
	}
	for i := range streams {
		if err := streams[i].advance(keys, numericKeys, len(config.Columns)); err != nil {
			return result, nil, err
		}
	}

	var differences []ShadowDifference
	for {
		var smallest []shadowKeyPart
		for _, stream := range streams {
			if stream.row != nil && (smallest == nil || compareShadowKeys(stream.key, smallest) < 0) {
				smallest = stream.key
			}
		}
		if smallest == nil {
			break
		}
		var rows [3]*ShadowRow
		for i := range streams {
			stream := &streams[i]
			if stream.row != nil && compareShadowKeys(stream.key, smallest) == 0 {
				rows[i] = stream.row
				if err := stream.advance(keys, numericKeys, len(config.Columns)); err != nil {
					return result, nil, err
				}
			}
		}
		if !equalShadowRows(rows[0], rows[1]) || !equalShadowRows(rows[0], rows[2]) {
			result.Equal = false
			result.Differences++
			if len(differences) < sampleLimit {
				key := make([]string, len(smallest))
				for i := range smallest {
					key[i] = smallest[i].text
				}
				differences = append(differences, ShadowDifference{
					RangeIndex: index, Key: key, Source: rows[0], Legacy: rows[1], Shadow: rows[2],
				})
			}
		}
	}
	result.Source, result.Legacy, result.Shadow = streams[0].digest(), streams[1].digest(), streams[2].digest()
	return result, differences, nil
}

// verifyShadowSourceMember 用当前查询连接读取源实例标识并与核对位点绑定的成员比对。
func verifyShadowSourceMember(ctx context.Context, conn *sql.Conn, cursor Cursor) error {
	separator := strings.LastIndexByte(cursor.SourceIdentity, '/')
	if separator <= 0 || separator == len(cursor.SourceIdentity)-1 {
		return herror.NewError("persistent shadow comparison requires a recorded CDC source member identity")
	}
	expected := cursor.SourceIdentity[separator+1:]
	if cursorFlavor(cursor) == mysql.MariaDBFlavor {
		// MariaDB 依赖 server_id 识别成员，部署时必须为各成员分配唯一的 ID。
		serverID, err := strconv.ParseUint(expected, 10, 32)
		if err != nil || serverID == 0 || !strings.Contains(cursor.SourceIdentity[:separator], "/") {
			return herror.NewError("persistent shadow comparison requires a recorded MariaDB source member identity")
		}
		var observed uint32
		var version string
		if err := conn.QueryRowContext(ctx, "SELECT @@GLOBAL.server_id, VERSION()").Scan(&observed, &version); err != nil {
			return herror.WrapMsg(err, "read MariaDB shadow comparison member identity")
		}
		if uint64(observed) == serverID && strings.Contains(strings.ToLower(version), "mariadb") {
			return nil
		}
	} else {
		var observed string
		if err := conn.QueryRowContext(ctx, "SELECT @@GLOBAL.server_uuid").Scan(&observed); err != nil {
			return herror.WrapMsg(err, "read MySQL shadow comparison member identity")
		}
		if observed == expected {
			return nil
		}
	}
	return herror.NewError("shadow comparison source connection resolved to a different binlog member")
}

// validateShadowRangeOrder 按主键实际类型校验区间结束边界排在起始边界之后。
func validateShadowRangeOrder(keyRange ShadowKeyRange, numericKeys []bool) error {
	bounds := make([][]shadowKeyPart, 2)
	for index, bound := range [][]any{keyRange.Start, keyRange.End} {
		for part, value := range bound {
			text, err := shadowBoundText(value)
			if err != nil {
				return err
			}
			key, err := parseShadowKeyPart(text, numericKeys[part])
			if err != nil {
				return herror.WrapMsg(err, "decode shadow comparison bound")
			}
			bounds[index] = append(bounds[index], key)
		}
	}
	if len(bounds[0]) != 0 && len(bounds[1]) != 0 && compareShadowKeys(bounds[0], bounds[1]) >= 0 {
		return herror.NewError("shadow comparison range end must sort after its start")
	}
	return nil
}

// shadowBoundText 将非空 SQL 标量边界转换为文本，供键比较使用。
func shadowBoundText(value any) (string, error) {
	switch value := value.(type) {
	case string:
		return value, nil
	case []byte:
		if value != nil {
			return string(value), nil
		}
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return fmt.Sprint(value), nil
	case bool:
		if value {
			return "1", nil
		}
		return "0", nil
	case time.Time:
		return value.Format("2006-01-02 15:04:05.999999999"), nil
	}
	return "", herror.NewError("shadow comparison range bounds require non-NULL SQL scalar values")
}

// shadowNumericKey 判断 SQL 列类型是否应按数值顺序比较主键。
func shadowNumericKey(typeName string) bool {
	for _, word := range strings.Fields(strings.ToUpper(typeName)) {
		name, _, _ := strings.Cut(word, "(")
		switch name {
		case "TINYINT", "SMALLINT", "MEDIUMINT", "INT", "INTEGER", "BIGINT", "LARGEINT", "DECIMAL", "NUMERIC", "FLOAT", "DOUBLE", "REAL", "YEAR":
			return true
		}
	}
	return false
}

var shadowDecimalKey = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]+)?$`)

// parseShadowKeyPart 解析单个复合键分量，数值分量使用有理数保持精度。
func parseShadowKeyPart(text string, numeric bool) (shadowKeyPart, error) {
	key := shadowKeyPart{text: text}
	if !numeric {
		return key, nil
	}
	if shadowDecimalKey.MatchString(text) {
		if rational, ok := new(big.Rat).SetString(text); ok {
			key.numeric = rational
			return key, nil
		}
	}
	return shadowKeyPart{}, herror.NewError("shadow comparison key cannot be interpreted as a number")
}

// advance 读取流中的下一行，校验键唯一且递增后更新行数与摘要。
func (stream *shadowStream) advance(keys []int, numeric []bool, columnCount int) error {
	stream.row = nil
	if !stream.rows.Next() {
		if err := stream.rows.Err(); err != nil {
			return herror.WrapMsg(err, "scan "+stream.label+" shadow comparison stream")
		}
		return nil
	}
	fields := make([]sql.NullString, columnCount)
	destinations := make([]any, columnCount)
	for i := range fields {
		destinations[i] = &fields[i]
	}
	if err := stream.rows.Scan(destinations...); err != nil {
		return herror.WrapMsg(err, "decode "+stream.label+" shadow comparison row")
	}

	key := make([]shadowKeyPart, len(keys))
	for i, index := range keys {
		field := fields[index]
		if !field.Valid {
			return herror.NewError(stream.label + " shadow comparison key contains NULL")
		}
		part, err := parseShadowKeyPart(field.String, numeric[i])
		if err != nil {
			return herror.WrapMsg(err, "decode "+stream.label+" comparison key")
		}
		key[i] = part
	}
	if stream.key != nil && compareShadowKeys(stream.key, key) >= 0 {
		return herror.NewError(stream.label + " shadow comparison keys are not unique and strictly ordered by the expected key ordering")
	}

	row := &ShadowRow{Values: make([]ShadowValue, columnCount)}
	for i, field := range fields {
		row.Values[i] = ShadowValue{Text: field.String, Valid: field.Valid}
	}
	stream.key, stream.row = key, row
	stream.count++
	writeShadowHash(stream.hash, row.Values)
	return nil
}

// compareShadowKeys 按复合键列顺序逐项比较数值或文本分量。
func compareShadowKeys(first, second []shadowKeyPart) int {
	for i, left := range first {
		right := second[i]
		var order int
		if left.numeric != nil {
			order = left.numeric.Cmp(right.numeric)
		} else {
			order = strings.Compare(left.text, right.text)
		}
		if order != 0 {
			return order
		}
	}
	return 0
}

// equalShadowRows 比较两行的投影值，区分缺失行、SQL NULL 和空文本。
func equalShadowRows(first, second *ShadowRow) bool {
	if first == nil || second == nil {
		return first == second
	}
	return slices.Equal(first.Values, second.Values)
}

// writeShadowHash 编码字段数量、空值标记和文本长度，再将字段值写入摘要。
func writeShadowHash(dst hash.Hash, values []ShadowValue) {
	var encoded [binary.MaxVarintLen64]byte
	length := binary.PutUvarint(encoded[:], uint64(len(values)))
	_, _ = dst.Write(encoded[:length])
	for _, value := range values {
		if !value.Valid {
			_, _ = dst.Write([]byte{0})
			continue
		}
		_, _ = dst.Write([]byte{1})
		length := binary.PutUvarint(encoded[:], uint64(len(value.Text)))
		_, _ = dst.Write(encoded[:length])
		_, _ = dst.Write([]byte(value.Text))
	}
}

// digest 返回当前数据流的行数及十六进制摘要。
func (stream *shadowStream) digest() ShadowDigest {
	return ShadowDigest{Rows: stream.count, SHA256: hex.EncodeToString(stream.hash.Sum(nil))}
}
