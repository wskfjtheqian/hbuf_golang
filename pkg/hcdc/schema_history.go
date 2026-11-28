package cdc

import (
	"encoding/json"
	"slices"
	"sort"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc/internal/mysqlsource"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc/internal/spool"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
)

type schemaVersion struct {
	Sequence uint64
	Cursor   Cursor
	Tables   []mysqlsource.Table
	Blocked  bool
}

// readBarrierCatalog 读取屏障对应的源端结构目录，并核对版本摘要和游标。
func readBarrierCatalog(history *spool.Store, record OrderedRecord) ([]byte, error) {
	catalog, err := history.Read(record.Sequence)
	if err != nil {
		return nil, herror.WrapMsg(err, "read staged CDC DDL schema version")
	}
	if digest(catalog) != record.StructureRevision {
		return nil, herror.NewError("staged CDC schema version does not match the DDL record")
	}
	var version schemaVersion
	if err := json.Unmarshal(catalog, &version); err != nil {
		return nil, herror.WrapMsg(err, "decode staged CDC DDL schema version")
	}
	if version.Sequence != record.Sequence || version.Cursor != record.Cursor || version.Blocked {
		return nil, herror.NewError("staged CDC schema version does not match the executable DDL barrier")
	}
	return catalog, nil
}

// readSchemaVersion 读取已落盘位点对应的结构版本，并清理尚未绑定持久屏障的版本。
func readSchemaVersion(store *spool.Store, baseline bootstrapState, durable Checkpoint, gtidAutoPosition bool) (schemaVersion, string, error) {
	version := schemaVersion{Cursor: baseline.Cursor, Tables: baseline.Tables}
	sequences, err := store.Sequences()
	if err != nil {
		return schemaVersion{}, "", err
	}
	for _, sequence := range sequences {
		// 崩溃可能发生在暂存结构之后、写入 DDL 记录之前。
		// 从新主重读同一 DDL 会得到不同的物理游标，因此只保留已绑定持久记录的版本。
		if sequence > durable.Sequence {
			if err := store.Remove(sequence); err != nil {
				return schemaVersion{}, "", herror.WrapMsg(err, "remove uncommitted CDC schema version")
			}
			continue
		}
		content, err := store.Read(sequence)
		if err != nil {
			return schemaVersion{}, "", err
		}
		var next schemaVersion
		if err := json.Unmarshal(content, &next); err != nil {
			return schemaVersion{}, "", herror.WrapMsg(err, "decode CDC schema version")
		}
		if next.Sequence != sequence {
			return schemaVersion{}, "", herror.NewError("CDC schema version has a sequence mismatch")
		}
		if err := validateCursorAdvance(version.Cursor, next.Cursor, gtidAutoPosition); err != nil {
			return schemaVersion{}, "", err
		}
		version = next
	}
	if version.Sequence != 0 && version.Cursor != durable.Cursor {
		if err := validateCursorAdvance(version.Cursor, durable.Cursor, gtidAutoPosition); err != nil {
			return schemaVersion{}, "", err
		}
	}
	content, err := json.Marshal(version)
	if err != nil {
		return schemaVersion{}, "", herror.Wrap(err)
	}
	revision := digest(content)
	if (durable.StructureRevision != "" && durable.StructureRevision != revision) ||
		(durable.StructureRevision == "" && version.Sequence != 0) {
		return schemaVersion{}, "", herror.NewError("CDC durable cursor does not match the recovered schema revision")
	}
	return version, revision, nil
}

// reclaimSchemaHistory 回收已交付结构之前的历史，只保留恢复锚点和所有更晚版本。
func reclaimSchemaHistory(store *spool.Store, delivered Checkpoint) error {
	if delivered.StructureRevision == "" {
		return nil
	}
	sequences, err := store.Sequences()
	if err != nil {
		return err
	}
	index := sort.Search(len(sequences), func(index int) bool { return sequences[index] > delivered.Sequence })
	if index <= 1 {
		return nil
	}
	anchor := sequences[index-1]
	catalog, err := store.Read(anchor)
	if err != nil {
		return err
	}
	if digest(catalog) != delivered.StructureRevision {
		return herror.NewError("CDC schema history anchor does not match the confirmed checkpoint")
	}
	return store.RemoveThrough(anchor - 1)
}

// planSchemaChange 将源端 DDL 转成目标屏障计划和可持久化的结构版本。
func planSchemaChange(database string, identity StreamIdentity, version schemaVersion, sequence uint64, transaction mysqlsource.Transaction) (schemaVersion, DDLPlan, []byte, error) {
	change := transaction.DDL
	index := slices.IndexFunc(version.Tables, func(table mysqlsource.Table) bool { return table.Ref == change.Before.Ref })
	if index < 0 || change.After.Ref != change.Before.Ref {
		return schemaVersion{}, DDLPlan{}, nil, herror.NewError("CDC DDL does not match a selected source table")
	}
	current := version.Tables[index]
	if current.DefaultCharset != change.Before.DefaultCharset || !slices.Equal(current.Columns, change.Before.Columns) ||
		!slices.Equal(current.PrimaryKey, change.Before.PrimaryKey) {
		return schemaVersion{}, DDLPlan{}, nil, herror.NewError("CDC DDL preimage differs from the active source schema version")
	}
	before, err := newTableLayout(database, change.Before)
	if err != nil {
		return schemaVersion{}, DDLPlan{}, nil, err
	}
	kind := DDLKind(change.Kind)
	switch kind {
	case DDLAddColumns, DDLTruncate, DDLDropColumn, DDLWidenInteger, DDLRenameColumn, DDLUnsupported:
	default:
		return schemaVersion{}, DDLPlan{}, nil, herror.NewError("source returned an unknown CDC DDL operation")
	}
	afterTable, reason := change.After, change.Reason
	after, err := newTableLayout(database, afterTable)
	cursor := Cursor(transaction.Cursor)
	plan := DDLPlan{
		Kind: kind, Before: before.target, After: after.target, RequiresApproval: change.RequiresApproval,
		Sequence: sequence, Cursor: cursor, Reason: reason,
	}
	if err == nil {
		switch kind {
		case DDLDropColumn, DDLWidenInteger, DDLRenameColumn:
			_, err = inspectDorisDDL(plan)
		}
	}
	if err != nil {
		// 需要重建的屏障保留最后一个有效解码布局。
		plan.Kind, plan.After, plan.Reason = DDLUnsupported, before.target, err.Error()
		afterTable = current
	}
	if plan.Kind == DDLUnsupported {
		plan.RequiresApproval = false
	}
	next := schemaVersion{Sequence: sequence, Cursor: cursor, Tables: slices.Clone(version.Tables), Blocked: plan.Kind == DDLUnsupported}
	next.Tables[index] = afterTable
	content, err := json.Marshal(next)
	if err != nil {
		return schemaVersion{}, DDLPlan{}, nil, herror.Wrap(err)
	}
	identityPayload, err := json.Marshal(struct {
		Identity StreamIdentity
		Txn      string
		Plan     DDLPlan
		Revision string
	}{identity, transaction.ID, plan, digest(content)})
	if err != nil {
		return schemaVersion{}, DDLPlan{}, nil, herror.Wrap(err)
	}
	plan.ID = "cdc_ddl_" + digest(identityPayload)
	return next, plan, content, nil
}
