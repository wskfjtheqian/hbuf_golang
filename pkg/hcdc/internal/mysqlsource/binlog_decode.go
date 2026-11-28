package mysqlsource

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/go-mysql-org/go-mysql/mysql"
	"github.com/go-mysql-org/go-mysql/replication"
	"github.com/klauspost/compress/zstd"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
)

const (
	maxBinlogEventBytes         = 64 << 20
	maxExpandedBinlogEventBytes = 8 << 20
)

type stagedBinlogDecoder struct {
	parser    *replication.BinlogParser
	eventType replication.EventType
}

// newStagedBinlogDecoder 在复制库提供原始事件后，使用与常规采集相同的列和时区设置解码。
func newStagedBinlogDecoder(flavor string) *stagedBinlogDecoder {
	decoder := &stagedBinlogDecoder{parser: replication.NewBinlogParser()}
	decoder.parser.SetFlavor(flavor)
	decoder.parser.SetTimestampStringLocation(time.UTC)
	decoder.parser.SetRenderJSONAsMySQLText(true)
	decoder.parser.SetVerifyChecksum(true)
	decoder.parser.SetPayloadDecoderConcurrency(1)
	decoder.parser.SetRowsEventDecodeFunc(decoder.decodeRows)
	return decoder
}

// decode 限制原始事件及压缩展开的大小，并保留复制库补齐的 MariaDB 位点。
func (d *stagedBinlogDecoder) decode(event *replication.BinlogEvent) (*replication.BinlogEvent, error) {
	if d == nil {
		return event, nil
	}
	if len(event.RawData) > maxBinlogEventBytes {
		return nil, herror.Wrap(fmt.Errorf("source binlog event exceeds %d bytes at position %d event=%s",
			maxBinlogEventBytes, event.Header.LogPos, event.Header.EventType))
	}
	d.eventType = event.Header.EventType
	if err := validateCompressedBinlogEvent(event); err != nil {
		return nil, herror.Wrap(fmt.Errorf("source binlog compression at position %d event=%s: %w", event.Header.LogPos, d.eventType, err))
	}
	decoded, err := d.parser.Parse(event.RawData)
	if err != nil {
		return nil, herror.WrapMsg(err, "decode bounded source binlog event")
	}
	decoded.Header.LogPos = event.Header.LogPos
	return decoded, nil
}

// decodeRows 核验 MariaDB 压缩行的展开长度后，交给原解码器处理位图和列值。
func (d *stagedBinlogDecoder) decodeRows(rows *replication.RowsEvent, data []byte) error {
	position, err := rows.DecodeHeader(data)
	if err != nil {
		return err
	}
	switch d.eventType {
	case replication.MARIADB_WRITE_ROWS_COMPRESSED_EVENT_V1, replication.MARIADB_UPDATE_ROWS_COMPRESSED_EVENT_V1,
		replication.MARIADB_DELETE_ROWS_COMPRESSED_EVENT_V1:
		if err := validateMariaDBCompressedData(data[position:]); err != nil {
			return err
		}
	}
	return rows.DecodeData(position, data)
}

// validateCompressedBinlogEvent 核验 MySQL 事务容器和 MariaDB 压缩查询，普通事件直接通过。
func validateCompressedBinlogEvent(event *replication.BinlogEvent) error {
	switch event.Header.EventType {
	case replication.TRANSACTION_PAYLOAD_EVENT:
		raw, ok := event.Event.(*replication.GenericEvent)
		if !ok {
			return errors.New("expected raw compressed MySQL transaction")
		}
		return validateMySQLCompressedTransaction(raw.Data)
	case replication.MARIADB_QUERY_COMPRESSED_EVENT:
		raw, ok := event.Event.(*replication.GenericEvent)
		if !ok || len(raw.Data) < 13 {
			return errors.New("invalid MariaDB compressed query header")
		}
		// 跳过查询状态变量、数据库名和分隔字节。
		position := 14 + int(raw.Data[8]) + int(binary.LittleEndian.Uint16(raw.Data[11:13]))
		if position >= len(raw.Data) {
			return errors.New("invalid MariaDB compressed query offset")
		}
		return validateMariaDBCompressedData(raw.Data[position:])
	}
	return nil
}

// validateMySQLCompressedTransaction 限制 Zstd 的内存和输出，并核验内层事件边界，拒绝嵌套压缩。
func validateMySQLCompressedTransaction(data []byte) error {
	fields := make(map[byte]uint64, 3)
	for {
		if len(data) == 0 {
			return errors.New("invalid MySQL compressed transaction header")
		}
		field := data[0]
		data = data[1:]
		if field == replication.OTW_PAYLOAD_HEADER_END_MARK {
			break
		}
		if len(data) == 0 || data[0] == 0 || data[0] > 9 || len(data) <= int(data[0]) {
			return errors.New("invalid MySQL compressed transaction header field")
		}
		width := int(data[0])
		data = data[1:]
		if _, exists := fields[field]; exists {
			return errors.New("repeated MySQL compressed transaction header field")
		}
		value, valid := mysqlCompressedFieldValue(data[:width])
		if !valid {
			return errors.New("invalid MySQL compressed transaction integer")
		}
		fields[field] = value
		data = data[width:]
	}
	compressionType, present := fields[replication.OTW_PAYLOAD_COMPRESSION_TYPE_FIELD]
	if !present || compressionType != replication.ZSTD || fields[replication.OTW_PAYLOAD_SIZE_FIELD] != uint64(len(data)) {
		return errors.New("invalid MySQL compressed transaction size or compression type")
	}
	uncompressed := fields[replication.OTW_PAYLOAD_UNCOMPRESSED_SIZE_FIELD]
	if uncompressed == 0 || uncompressed > maxExpandedBinlogEventBytes {
		return fmt.Errorf("expanded MySQL binlog transaction exceeds the %d byte limit or has no size", maxExpandedBinlogEventBytes)
	}
	decoder, err := zstd.NewReader(nil, zstd.WithDecoderConcurrency(1), zstd.WithDecoderMaxMemory(maxExpandedBinlogEventBytes))
	if err != nil {
		return err
	}
	defer decoder.Close()
	payload, err := decoder.DecodeAll(data, nil)
	if err != nil {
		return err
	}
	if uint64(len(payload)) != uncompressed {
		return fmt.Errorf("expanded MySQL binlog transaction length differs from its header: declared %d, actual %d", uncompressed, len(payload))
	}
	for len(payload) != 0 {
		if len(payload) < replication.EventHeaderSize {
			return errors.New("invalid event header in MySQL compressed transaction")
		}
		size := int(binary.LittleEndian.Uint32(payload[9:13]))
		if size < replication.EventHeaderSize || size > len(payload) {
			return errors.New("invalid event length in MySQL compressed transaction")
		}
		event := replication.EventType(payload[4])
		if event == replication.XID_EVENT && size == len(payload) {
			return nil
		}
		switch event {
		case replication.QUERY_EVENT, replication.TABLE_MAP_EVENT, replication.ROWS_QUERY_EVENT,
			replication.WRITE_ROWS_EVENTv0, replication.WRITE_ROWS_EVENTv1, replication.WRITE_ROWS_EVENTv2,
			replication.UPDATE_ROWS_EVENTv0, replication.UPDATE_ROWS_EVENTv1, replication.UPDATE_ROWS_EVENTv2,
			replication.DELETE_ROWS_EVENTv0, replication.DELETE_ROWS_EVENTv1, replication.DELETE_ROWS_EVENTv2,
			replication.PARTIAL_UPDATE_ROWS_EVENT:
		default:
			return fmt.Errorf("unsupported %s event in MySQL compressed transaction", event)
		}
		payload = payload[size:]
	}
	return errors.New("MySQL compressed transaction is missing its final commit event")
}

// mysqlCompressedFieldValue 解码 MySQL 事件头中的长度编码整数。
func mysqlCompressedFieldValue(data []byte) (uint64, bool) {
	var width int
	switch data[0] {
	case 0xfc:
		width = 3
	case 0xfd:
		width = 4
	case 0xfe:
		width = 9
	default:
		if data[0] < 0xfb && len(data) == 1 {
			return uint64(data[0]), true
		}
		return 0, false
	}
	if len(data) != width {
		return 0, false
	}
	return mysql.FixedLengthInt(data[1:]), true
}

// validateMariaDBCompressedData 核对 zlib 声明和实际长度，限制复制库的单次内存分配。
func validateMariaDBCompressedData(data []byte) error {
	if len(data) == 0 {
		return errors.New("MariaDB compression header is missing")
	}
	width := int(data[0] & 7)
	if width == 0 || len(data) <= 1+width {
		return errors.New("invalid MariaDB compression header")
	}
	size := mysql.BFixedLengthInt(data[1 : 1+width])
	if size == 0 || size > maxExpandedBinlogEventBytes {
		return fmt.Errorf("expanded MariaDB binlog event exceeds the %d byte limit or has no size", maxExpandedBinlogEventBytes)
	}
	reader, err := zlib.NewReader(bytes.NewReader(data[1+width:]))
	if err != nil {
		return err
	}
	defer reader.Close()
	decoded, err := io.CopyN(io.Discard, reader, int64(size)+1)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if uint64(decoded) != size {
		return errors.New("expanded MariaDB binlog event length differs from its header")
	}
	return nil
}
