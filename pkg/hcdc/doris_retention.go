package hcdc

import (
	"context"
	"database/sql"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
)

const defaultControlRetentionRecords = 4096

type dorisLoadHistory struct {
	StreamKey string
	Sequence  uint64
}

type dorisLoadHistoryKey struct{}

// withDorisLoadHistory 将导入回执绑定到能够覆盖该批次的检查点序号。
func withDorisLoadHistory(ctx context.Context, identity StreamIdentity, sequence uint64) context.Context {
	return context.WithValue(ctx, dorisLoadHistoryKey{}, dorisLoadHistory{dorisStreamKey(identity), sequence})
}

// trackLoadReceipt 在发出导入前登记归属。旧版本中没有归属凭据的回执继续保留。
func (d *DorisTarget) trackLoadReceipt(ctx context.Context, label string) error {
	entry, ok := ctx.Value(dorisLoadHistoryKey{}).(dorisLoadHistory)
	if !ok {
		return nil
	}
	_, err := d.db.ExecContext(ctx, "INSERT INTO "+d.controlTable("cdc_load_history")+" VALUES (?, ?, ?)", entry.StreamKey, entry.Sequence, label)
	if err != nil {
		return wrapDorisRequestError(err, "index CDC load receipt for retention")
	}
	return nil
}

// collectControlHistory 周期性回收已验证且早于当前恢复窗口的控制数据。
func (d *DorisTarget) collectControlHistory(ctx context.Context, checkpoint Checkpoint) {
	retain := d.config.ControlRetentionRecords
	if retain == 0 {
		retain = defaultControlRetentionRecords
	}
	if checkpoint.Sequence <= retain || checkpoint.Sequence%min(retain, 128) != 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := d.pruneControlHistory(ctx, dorisStreamKey(checkpoint.Identity), checkpoint.Sequence-retain); err != nil {
		hlog.Warn(ctx, "CDC control history cleanup failed source_id=%s sequence=%d error=%v", checkpoint.Identity.SourceID, checkpoint.Sequence, err)
	}
}

// pruneControlHistory 保留快照基线、最近检查点及其可能引用的第一个结构版本。
func (d *DorisTarget) pruneControlHistory(ctx context.Context, streamKey string, cutoff uint64) error {
	history := d.controlTable("cdc_load_history")
	receipts := d.controlTable("load_receipts")
	// 先删已验证批次的探测预算，再删回执。任何一步中断都保留索引供重试。
	queries := []string{
		"DELETE FROM " + d.controlTable("load_recheck_deadlines") + " deadline USING " + history + " history JOIN " + receipts + " receipt ON receipt.load_label = history.load_label" +
			" WHERE deadline.load_label = history.load_label AND history.stream_key = ? AND history.sequence_no <= ? AND receipt.verified = TRUE",
		"DELETE FROM " + receipts + " receipt USING " + history + " history" +
			" WHERE receipt.load_label = history.load_label AND history.stream_key = ? AND history.sequence_no <= ? AND receipt.verified = TRUE",
		"DELETE FROM " + history + " entry USING (SELECT history.stream_key, history.sequence_no, history.load_label FROM " + history + " history" +
			" LEFT JOIN " + receipts + " receipt ON history.load_label = receipt.load_label WHERE history.stream_key = ? AND history.sequence_no <= ? AND receipt.load_label IS NULL) removed" +
			" WHERE entry.stream_key = removed.stream_key AND entry.sequence_no = removed.sequence_no AND entry.load_label = removed.load_label",
		"DELETE FROM " + d.controlTable("checkpoints") + " WHERE stream_key = ? AND sequence_no > 0 AND sequence_no <= ?",
	}
	for _, query := range queries {
		if _, err := d.db.ExecContext(ctx, query, streamKey, cutoff); err != nil {
			return wrapDorisRequestError(err, "prune CDC control records")
		}
	}
	versions := d.controlTable("schema_versions")
	var anchor sql.NullInt64
	err := d.db.QueryRowContext(ctx, "SELECT MAX(sequence_no) FROM "+versions+" WHERE stream_key = ? AND sequence_no <= ?", streamKey, cutoff).Scan(&anchor)
	if err != nil {
		return wrapDorisRequestError(err, "find CDC schema retention baseline")
	}
	if anchor.Valid && anchor.Int64 > 0 {
		if _, err := d.db.ExecContext(ctx, "DELETE FROM "+versions+" WHERE stream_key = ? AND sequence_no > 0 AND sequence_no < ?", streamKey, anchor.Int64); err != nil {
			return wrapDorisRequestError(err, "prune CDC schema history")
		}
	}
	return nil
}
