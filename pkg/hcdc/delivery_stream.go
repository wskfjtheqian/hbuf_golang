package hcdc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"

	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
)

// stagedRecord 持有一笔已规划事务的完整封装负载，等待按序持久发布。
type stagedRecord struct {
	record OrderedRecord
	file   *os.File
	size   int64
	limit  int64
}

// stageRecord 将已提交事务的分批计划写入临时文件，保留原有 CDC1 JSON 格式。
func (d *Delivery) stageRecord(record OrderedRecord, limit int64, prepare func(func(LoadBatch) error) (uint64, error)) (_ *stagedRecord, resultErr error) {
	file, err := d.store.CreateTemporaryFile()
	if err != nil {
		return nil, err
	}
	staged := &stagedRecord{file: file, record: record, limit: limit}
	defer func() {
		if resultErr != nil {
			staged.close()
		}
	}()
	meta := struct {
		bufferedOrderedRecord
		Batches []LoadBatch `json:"Batches,omitempty"`
	}{bufferedOrderedRecord: bufferedOrderedRecord{OrderedRecord: record, ManifestHash: d.manifestHash}}
	meta.ZeroDateRows = 0
	prefix, err := json.Marshal(meta)
	if err != nil {
		return nil, herror.WrapMsg(err, "encode buffered CDC record header")
	}
	if _, err := staged.Write(prefix[:len(prefix)-1]); err != nil {
		return nil, err
	}
	if _, err := io.WriteString(staged, `,"Batches":[`); err != nil {
		return nil, err
	}
	encoder := json.NewEncoder(staged)
	mapped, err := prepare(func(batch LoadBatch) error {
		if err := validateBatch(batch); err != nil {
			return err
		}
		index := len(staged.record.Batches)
		batch.Checksum = digest(batch.Payload)
		label, err := d.batchLabel(record, batch.TargetTable, index)
		if err != nil {
			return err
		}
		batch.Label = label
		if index > 0 {
			if _, err := io.WriteString(staged, ","); err != nil {
				return err
			}
		}
		if err := encoder.Encode(batch); err != nil {
			return herror.WrapMsg(err, "encode buffered CDC load batch")
		}
		batch.Payload = nil
		staged.record.Batches = append(staged.record.Batches, batch)
		return nil
	})
	if err != nil {
		return nil, err
	}
	if mapped > math.MaxUint64-record.ZeroDateRows {
		return nil, herror.NewError("CDC zero date row count overflow")
	}
	staged.record.ZeroDateRows += mapped
	if err := validateRecordHeader(staged.record); err != nil {
		return nil, err
	}
	suffix := `],"ZeroDateRows":` + strconv.FormatUint(staged.record.ZeroDateRows, 10) + `}`
	if _, err := io.WriteString(staged, suffix); err != nil {
		return nil, err
	}
	return staged, nil
}

// Write 在当前事务的持久负载达到容量限制前停止写入。
func (s *stagedRecord) Write(data []byte) (int, error) {
	if s.limit > 0 && int64(len(data)) > s.limit-s.size {
		return 0, herror.Wrap(fmt.Errorf("%w: encoded durable record limit %d bytes", ErrTransactionTooLarge, s.limit))
	}
	written, err := s.file.Write(data)
	s.size += int64(written)
	if err != nil {
		return written, herror.Wrap(err)
	}
	return written, nil
}

// close 清理未发布的暂存负载。
func (s *stagedRecord) close() {
	_ = s.file.Close()
	_ = os.Remove(s.file.Name())
}

// appendStagedRecord 在积压预算内原子发布已分批的事务，并同时推进持久游标。
func (d *Delivery) appendStagedRecord(staged *stagedRecord) error {
	if d.closing.Load() {
		return ErrDeliveryClosed
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closing.Load() {
		return ErrDeliveryClosed
	}
	if err := d.validateNext(staged.record); err != nil {
		return err
	}
	if staged.limit > 0 && d.status.PendingBytes > staged.limit-staged.size {
		return errBufferFull
	}
	if err := d.store.AppendFile(staged.record.Sequence, staged.file, staged.size); err != nil {
		return err
	}
	d.status.Durable = d.checkpoint(staged.record)
	d.status.PendingBytes += staged.size
	return nil
}

// decodeBufferedRecord 逐批读取旧版和新版缓冲，避免在内存中保留整个事务的导入负载。
func decodeBufferedRecord(input io.Reader, visit func(LoadBatch) error) (bufferedOrderedRecord, error) {
	decoder := json.NewDecoder(input)
	if err := expectBufferedRecordToken(decoder, json.Delim('{')); err != nil {
		return bufferedOrderedRecord{}, err
	}
	fields := make(map[string]json.RawMessage)
	var batches []LoadBatch
	var seenBatches bool
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return bufferedOrderedRecord{}, herror.WrapMsg(err, "decode buffered CDC field name")
		}
		name, ok := token.(string)
		if !ok {
			return bufferedOrderedRecord{}, herror.NewError("buffered CDC record contains an invalid field name")
		}
		if name == "Batches" {
			if seenBatches {
				return bufferedOrderedRecord{}, herror.NewError("buffered CDC record repeats its batch list")
			}
			seenBatches = true
			batches, err = readBufferedBatches(decoder, visit)
			if err != nil {
				return bufferedOrderedRecord{}, err
			}
			continue
		}
		if _, exists := fields[name]; exists {
			return bufferedOrderedRecord{}, herror.NewError("buffered CDC record repeats a field")
		}
		var field json.RawMessage
		if err := decoder.Decode(&field); err != nil {
			return bufferedOrderedRecord{}, herror.WrapMsg(err, "decode buffered CDC record field")
		}
		fields[name] = field
	}
	if err := expectBufferedRecordToken(decoder, json.Delim('}')); err != nil {
		return bufferedOrderedRecord{}, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return bufferedOrderedRecord{}, herror.NewError("buffered CDC record contains extra data")
	}
	payload, err := json.Marshal(fields)
	if err != nil {
		return bufferedOrderedRecord{}, herror.WrapMsg(err, "encode buffered CDC record metadata")
	}
	var record bufferedOrderedRecord
	if err := json.Unmarshal(payload, &record); err != nil {
		return bufferedOrderedRecord{}, herror.WrapMsg(err, "decode buffered CDC record metadata")
	}
	record.Batches = batches
	return record, nil
}

// readBufferedBatches 校验每组负载，并仅保留后续校验需要的批次元数据。
func readBufferedBatches(decoder *json.Decoder, visit func(LoadBatch) error) ([]LoadBatch, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, herror.WrapMsg(err, "decode buffered CDC batch list")
	}
	if token == nil {
		return nil, nil
	}
	if token != json.Delim('[') {
		return nil, herror.NewError("buffered CDC record has an invalid batch list")
	}
	var batches []LoadBatch
	for decoder.More() {
		var batch LoadBatch
		if err := decoder.Decode(&batch); err != nil {
			return nil, herror.WrapMsg(err, "decode buffered CDC load batch")
		}
		if err := validateBatch(batch); err != nil {
			return nil, err
		}
		if batch.Checksum != digest(batch.Payload) {
			return nil, herror.NewError("buffered CDC load batch has an invalid checksum")
		}
		if visit != nil {
			if err := visit(batch); err != nil {
				return nil, err
			}
		}
		batch.Payload = nil
		batches = append(batches, batch)
	}
	return batches, expectBufferedRecordToken(decoder, json.Delim(']'))
}

// expectBufferedRecordToken 校验 JSON 流的结构边界。
func expectBufferedRecordToken(decoder *json.Decoder, expected json.Delim) error {
	token, err := decoder.Token()
	if err != nil {
		return herror.WrapMsg(err, "decode buffered CDC record boundary")
	}
	if token != expected {
		return herror.NewError("buffered CDC record has an invalid JSON boundary")
	}
	return nil
}
