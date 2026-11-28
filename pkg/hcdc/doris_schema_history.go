package cdc

import (
	"context"
	"database/sql"
	"errors"
	"math"

	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
)

// ensureSchemaHistory 创建保存 Doris 目标结构历史的控制表。
func (d *DorisTarget) ensureSchemaHistory(ctx context.Context) error {
	query := "CREATE TABLE IF NOT EXISTS " + d.controlTable("schema_versions") + " (" +
		"stream_key CHAR(64) NOT NULL, sequence_no BIGINT NOT NULL, " +
		"structure_revision CHAR(64) NOT NULL, source_catalog STRING NOT NULL) " +
		"ENGINE=OLAP UNIQUE KEY(stream_key, sequence_no) DISTRIBUTED BY HASH(stream_key) BUCKETS 1" + dorisUniqueProperties
	if _, err := d.db.ExecContext(ctx, query); err != nil {
		return wrapDorisRequestError(err, "create Doris CDC schema history table")
	}
	return nil
}

// persistSchemaVersion 持久化指定流的结构版本，并核对读回的摘要和目录。
func (d *DorisTarget) persistSchemaVersion(ctx context.Context, identity StreamIdentity, sequence uint64, revision string, catalog []byte) error {
	if sequence > math.MaxInt64 || revision != digest(catalog) {
		return herror.NewError("invalid CDC schema version sequence or checksum")
	}
	key := dorisStreamKey(identity)
	verify := func() (bool, error) {
		var actualRevision, actualCatalog string
		query := "SELECT structure_revision, source_catalog FROM " + d.controlTable("schema_versions") +
			" WHERE stream_key = ? AND sequence_no = ?"
		err := d.db.QueryRowContext(ctx, query, key, sequence).Scan(&actualRevision, &actualCatalog)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, wrapDorisRequestError(err, "read Doris CDC schema version")
		}
		if actualRevision != revision || actualCatalog != string(catalog) {
			return false, herror.NewError("Doris schema version conflicts with the source catalog")
		}
		return true, nil
	}
	saved, err := verify()
	if err != nil || saved {
		return err
	}
	_, writeErr := d.db.ExecContext(ctx, "INSERT INTO "+d.controlTable("schema_versions")+" VALUES (?, ?, ?, ?)",
		key, sequence, revision, string(catalog))
	saved, readErr := verify()
	if saved {
		return nil
	}
	if readErr != nil && !errors.Is(readErr, errDorisTargetUnavailable) {
		return herror.WrapMsg(readErr, "verify Doris CDC schema version")
	}
	if writeErr != nil {
		return wrapDorisRequestError(writeErr, "write Doris CDC schema version")
	}
	if readErr != nil {
		return readErr
	}
	return herror.NewError("Doris CDC schema version could not be verified")
}

// verifySchemaRevision 检查检查点引用的结构版本是否与目标保存的记录一致。
func (d *DorisTarget) verifySchemaRevision(ctx context.Context, checkpoint Checkpoint) error {
	if checkpoint.StructureRevision == "" {
		// 早期检查点只记录了不可变的初始快照。
		return nil
	}
	var catalog string
	query := "SELECT source_catalog FROM " + d.controlTable("schema_versions") +
		" WHERE stream_key = ? AND structure_revision = ? AND sequence_no <= ?"
	err := d.db.QueryRowContext(ctx, query, dorisStreamKey(checkpoint.Identity),
		checkpoint.StructureRevision, checkpoint.Sequence).Scan(&catalog)
	if err != nil {
		return wrapDorisRequestError(err, "verify Doris checkpoint schema version")
	}
	if digest([]byte(catalog)) != checkpoint.StructureRevision {
		return herror.NewError("Doris checkpoint schema version checksum mismatch")
	}
	return nil
}
