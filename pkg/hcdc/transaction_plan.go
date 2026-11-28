package cdc

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc/internal/mysqlsource"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
)

const (
	defaultPendingBytes = 256 << 20
	transactionRunBytes = 4 << 20
)

// ErrTransactionTooLarge 表示单个事务超过暂存或持久缓冲预算，游标尚未推进。
var ErrTransactionTooLarge = errors.New("CDC transaction exceeded its byte budget")

type transactionRowKey struct {
	Table int
	Key   string
}

type transactionRow struct {
	transactionRowKey
	Row            DorisRow
	ZeroDateMapped bool
}

type stagedTransactionRow struct {
	row    transactionRow
	data   []byte
	memory int64
}

// transactionPlan 以有界内存暂存行变更，通过有序磁盘归并保留每个主键的最终状态。
// runs 的较高层保存较早的变更，合并时保留较新的同键行。
type transactionPlan struct {
	ctx        context.Context
	createTemp func() (*os.File, error)
	layouts    []tableLayout
	byTable    map[mysqlsource.TableRef]int
	rows       map[transactionRowKey]stagedTransactionRow
	runs       []string
	memory     int64
	written    int64
	limit      int64
	runLimit   int64
	planErr    error
}

// newTransactionPlan 按表目录初始化一笔尚未提交的事务。
func newTransactionPlan(ctx context.Context, layouts []tableLayout, limit int64, createTemp func() (*os.File, error)) *transactionPlan {
	p := &transactionPlan{
		ctx: ctx, createTemp: createTemp, rows: make(map[transactionRowKey]stagedTransactionRow),
		limit: limit, runLimit: transactionRunBytes,
	}
	p.setLayouts(layouts)
	return p
}

// setLayouts 在已持久化的 DDL 边界更新后续事务的结构目录。
func (p *transactionPlan) setLayouts(layouts []tableLayout) {
	p.layouts = layouts
	p.byTable = make(map[mysqlsource.TableRef]int, len(layouts))
	for index, layout := range layouts {
		p.byTable[layout.source.Ref] = index
	}
}

// stage 暂存选表的行映像，行映射错误延迟到提交时报告，以便正常丢弃回滚的事务。
func (p *transactionPlan) stage(change mysqlsource.Change) error {
	if err := p.ctx.Err(); err != nil {
		return herror.Wrap(err)
	}
	if p.planErr != nil {
		return nil
	}
	table, ok := p.byTable[change.Table]
	if !ok {
		return herror.NewError("CDC transaction refers to an unknown source table")
	}
	layout := p.layouts[table]
	var afterKey string
	if change.After != nil {
		row, key, err := encodeTransactionRow(layout, table, change.After, false)
		if err != nil {
			p.planErr = err
			return nil
		}
		afterKey = key
		if err := p.addRow(row); err != nil {
			return err
		}
	}
	if change.Before != nil {
		row, key, err := encodeTransactionRow(layout, table, change.Before, true)
		if err != nil {
			p.planErr = err
			return nil
		}
		if change.After == nil || key != afterKey {
			return p.addRow(row)
		}
	}
	return nil
}

// encodeTransactionRow 依据当前目录转换单行，并核对主键与 Doris 单行上限。
func encodeTransactionRow(layout tableLayout, table int, image []any, deleteRow bool) (transactionRow, string, error) {
	row, mapped, err := layout.encodeRow(image, deleteRow)
	if err != nil {
		return transactionRow{}, "", err
	}
	key, err := layout.rowKey(row)
	if err != nil {
		return transactionRow{}, "", err
	}
	if err := layout.checkLoadRowSize(encodedDorisRowSize(row)); err != nil {
		return transactionRow{}, "", err
	}
	return transactionRow{transactionRowKey: transactionRowKey{Table: table, Key: key}, Row: row, ZeroDateMapped: mapped}, key, nil
}

// addRow 按实际暂存编码量计入预算，并在内存达到阈值时写入一个有序段。
func (p *transactionPlan) addRow(row transactionRow) error {
	data, err := json.Marshal(row)
	if err != nil {
		return herror.WrapMsg(err, "encode staged CDC change")
	}
	size := int64(len(data)) + 1
	if p.limit > 0 && size > p.limit-p.written {
		return herror.Wrap(fmt.Errorf("%w: source table %s.%s, limit %d bytes", ErrTransactionTooLarge,
			p.layouts[row.Table].source.Ref.Schema, p.layouts[row.Table].source.Ref.Name, p.limit))
	}
	p.written += size
	key := row.transactionRowKey
	previous, exists := p.rows[key]
	if !exists {
		p.memory += int64(len(key.Key)) + 128 // 计入键副本及映射开销。
	}
	memory := int64(len(data) + encodedDorisRowSize(row.Row) + 32*len(row.Row.Values))
	p.memory += memory - previous.memory
	p.rows[key] = stagedTransactionRow{row: row, data: data, memory: memory}
	if p.memory >= p.runLimit {
		return p.flush()
	}
	return nil
}

// sortedKeys 以稳定的表目录和主键顺序排列内存变更。
func (p *transactionPlan) sortedKeys() []transactionRowKey {
	keys := make([]transactionRowKey, 0, len(p.rows))
	for key := range p.rows {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, compareTransactionKeys)
	return keys
}

// compareTransactionKeys 比较表目录序号，再比较该表中的主键。
func compareTransactionKeys(first, second transactionRowKey) int {
	if order := cmp.Compare(first.Table, second.Table); order != 0 {
		return order
	}
	return cmp.Compare(first.Key, second.Key)
}

// flush 将本段落盘并合并相邻的同层段，限制同时打开的文件数。
func (p *transactionPlan) flush() error {
	path, err := p.writeRun(func(output *bufio.Writer) error {
		for _, key := range p.sortedKeys() {
			if err := writeTransactionRunRow(output, p.rows[key].data); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	p.rows = make(map[transactionRowKey]stagedTransactionRow)
	p.memory = 0
	for level, older := range p.runs {
		if older == "" {
			p.runs[level] = path
			return nil
		}
		merged, err := p.mergeRuns(older, path)
		_ = os.Remove(path)
		if err != nil {
			return err
		}
		_ = os.Remove(older)
		p.runs[level] = ""
		path = merged
	}
	p.runs = append(p.runs, path)
	return nil
}

// writeRun 完整写入一个可丢弃的暂存段，错误时清理不完整文件。
func (p *transactionPlan) writeRun(write func(*bufio.Writer) error) (string, error) {
	file, err := p.createTemp()
	if err != nil {
		return "", err
	}
	output := bufio.NewWriter(file)
	err = write(output)
	if err == nil {
		err = output.Flush()
	}
	if err := errors.Join(err, file.Close()); err != nil {
		_ = os.Remove(file.Name())
		return "", herror.WrapMsg(err, "write staged CDC transaction run")
	}
	return file.Name(), nil
}

// writeTransactionRunRow 使用 JSON 行边界保存暂存编码。
func writeTransactionRunRow(output *bufio.Writer, data []byte) error {
	if _, err := output.Write(data); err != nil {
		return err
	}
	return output.WriteByte('\n')
}

type transactionRunReader struct {
	input *bufio.Reader
	key   transactionRowKey
	data  []byte
}

// next 读取当前段的下一行，只解析排序键。
func (r *transactionRunReader) next() error {
	data, err := r.input.ReadBytes('\n')
	if errors.Is(err, io.EOF) && len(data) == 0 {
		r.data = nil
		return nil
	}
	if err != nil {
		return herror.WrapMsg(err, "read staged CDC transaction run")
	}
	r.data = data[:len(data)-1]
	if err := json.Unmarshal(r.data, &r.key); err != nil {
		return herror.WrapMsg(err, "decode staged CDC transaction key")
	}
	return nil
}

// mergeRuns 顺序合并相邻两段，较新的同键行覆盖较早行。
func (p *transactionPlan) mergeRuns(olderPath, newerPath string) (string, error) {
	older, err := os.Open(olderPath)
	if err != nil {
		return "", herror.Wrap(err)
	}
	defer older.Close()
	newer, err := os.Open(newerPath)
	if err != nil {
		return "", herror.Wrap(err)
	}
	defer newer.Close()
	left := transactionRunReader{input: bufio.NewReader(older)}
	right := transactionRunReader{input: bufio.NewReader(newer)}
	if err := left.next(); err != nil {
		return "", err
	}
	if err := right.next(); err != nil {
		return "", err
	}
	return p.writeRun(func(output *bufio.Writer) error {
		for left.data != nil || right.data != nil {
			if err := p.ctx.Err(); err != nil {
				return err
			}
			row := &right
			if left.data != nil {
				if right.data == nil {
					row = &left
				} else {
					switch compareTransactionKeys(left.key, right.key) {
					case -1:
						row = &left
					case 0:
						if err := left.next(); err != nil {
							return err
						}
					}
				}
			}
			if err := writeTransactionRunRow(output, row.data); err != nil {
				return err
			}
			if err := row.next(); err != nil {
				return err
			}
		}
		return nil
	})
}

// eachRow 按稳定顺序遍历归并后的最终行，不在内存中保留完整事务。
func (p *transactionPlan) eachRow(consume func(transactionRow) error) error {
	decode := func(data []byte) error {
		if err := p.ctx.Err(); err != nil {
			return herror.Wrap(err)
		}
		var row transactionRow
		if err := json.Unmarshal(data, &row); err != nil {
			return herror.WrapMsg(err, "decode staged CDC transaction row")
		}
		return consume(row)
	}
	if len(p.runs) == 0 {
		for _, key := range p.sortedKeys() {
			if err := p.ctx.Err(); err != nil {
				return herror.Wrap(err)
			}
			if err := consume(p.rows[key].row); err != nil {
				return err
			}
		}
		return nil
	}
	if len(p.rows) != 0 {
		if err := p.flush(); err != nil {
			return err
		}
	}
	path := ""
	defer func() { _ = os.Remove(path) }()
	for _, newer := range slices.Backward(p.runs) {
		if newer == "" {
			continue
		}
		if path == "" {
			path = newer
			continue
		}
		merged, err := p.mergeRuns(path, newer)
		if err != nil {
			return err
		}
		_ = os.Remove(path)
		_ = os.Remove(newer)
		path = merged
	}
	file, err := os.Open(path)
	if err != nil {
		return herror.Wrap(err)
	}
	defer file.Close()
	run := transactionRunReader{input: bufio.NewReader(file)}
	for {
		if err := run.next(); err != nil {
			return err
		}
		if run.data == nil {
			return nil
		}
		if err := decode(run.data); err != nil {
			return err
		}
	}
}

// batches 在提交后分批产生导入负载，并统计最终受零日期映射影响的行数。
func (p *transactionPlan) batches(consume func(LoadBatch) error) (uint64, error) {
	if p.planErr != nil {
		return 0, p.planErr
	}
	var zeroDateRows uint64
	table, bytes := 0, 0
	var rows []DorisRow
	flush := func() error {
		if len(rows) == 0 {
			return nil
		}
		batch, err := p.layouts[table].batch(rows)
		if err != nil {
			return err
		}
		clear(rows)
		rows = rows[:0]
		bytes = 0
		return consume(batch)
	}
	if err := p.eachRow(func(row transactionRow) error {
		size := encodedDorisRowSize(row.Row)
		if len(rows) > 0 && (table != row.Table || bytes+size > defaultDorisLoadBatchBytes) {
			if err := flush(); err != nil {
				return err
			}
		}
		table = row.Table
		rows = append(rows, row.Row)
		bytes += size
		if row.ZeroDateMapped {
			zeroDateRows++
		}
		return nil
	}); err != nil {
		return 0, err
	}
	return zeroDateRows, flush()
}

// reset 清理这次订阅的未提交行，或释放已经持久化的事务暂存。
func (p *transactionPlan) reset() {
	for _, path := range p.runs {
		if path != "" {
			_ = os.Remove(path)
		}
	}
	p.runs = nil
	clear(p.rows)
	p.memory, p.written, p.planErr = 0, 0, nil
}
