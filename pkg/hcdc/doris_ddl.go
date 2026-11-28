package cdc

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
)

const (
	dorisDDLResolutionTimeout = 30 * time.Second
	defaultDorisDDLJobTimeout = time.Hour
)

type dorisDDLReceipt struct {
	PlanDigest   string
	JobFence     uint64
	PartitionIDs []uint64
	SchemaJobID  uint64
	JobDeadline  int64
	Verified     bool
}

type dorisDDLJobWitness struct {
	PartitionIDs []uint64 `json:"partition_ids"`
	SchemaJobID  uint64   `json:"schema_job_id"`
	JobDeadline  int64    `json:"deadline_ms,omitempty"`
}

type dorisSchemaJob struct {
	ID    uint64
	State string
}

type dorisSchemaJobStatus struct {
	Latest     dorisSchemaJob
	Active     int
	AfterFence int
}

type dorisDDLAction struct {
	Additions []DorisColumn
	OldColumn string
	NewColumn string
}

// ApplyDDL 应用并校验队首的 DDL 操作。调用方须持有目标的独占写入权，
// 并在确认该操作及对应的交付检查点后放行后续记录。
// 已确认原结构作业仍在运行时返回 ErrLoadPending，随后可按同一回执继续查询。
func (d *DorisTarget) ApplyDDL(ctx context.Context, plan DDLPlan) error {
	action, err := validateDorisDDL(plan)
	if err != nil {
		return err
	}
	if strings.EqualFold(d.config.ControlDatabase, plan.Before.Database) {
		return herror.NewError("Doris DDL target conflicts with the control database")
	}
	ctx, cancel := context.WithTimeout(ctx, dorisDDLResolutionTimeout)
	defer cancel()
	if err := d.ensureDDLReceipts(ctx); err != nil {
		return err
	}

	encoded, err := json.Marshal(plan)
	if err != nil {
		return herror.WrapMsg(err, "encode Doris DDL plan")
	}
	checksum := digest(encoded)
	receipt, err := d.readDDLReceipt(ctx, plan.ID, checksum)
	if err != nil {
		return err
	}
	if receipt != nil && receipt.Verified {
		return nil
	}

	table := plan.Before
	qualified := quoteDoris(table.Database) + "." + quoteDoris(table.Name)
	if err := d.verifyDorisUniqueTable(ctx, qualified); err != nil {
		return err
	}
	var submissionErr error
	if receipt == nil {
		yes, err := d.dorisSchemaMatches(ctx, plan.Before)
		if err != nil {
			return err
		}
		if !yes {
			return herror.NewError("Doris schema differs from the expected pre-DDL version")
		}
		jobs, err := d.dorisSchemaJobs(ctx, table, 0)
		if err != nil {
			return err
		}
		if jobs.Active != 0 {
			return herror.NewError("Doris target has another active schema change")
		}

		receipt = &dorisDDLReceipt{PlanDigest: checksum, JobFence: jobs.Latest.ID}
		switch plan.Kind {
		case DDLTruncate, DDLDropColumn, DDLRenameColumn:
			receipt.PartitionIDs, err = d.dorisPartitionIDs(ctx, qualified)
			if err != nil {
				return err
			}
		}
		if err := d.writeDDLReceipt(ctx, plan.ID, *receipt); err != nil {
			return err
		}
		hlog.Info(ctx, "Doris DDL intent stored ddl_id=%s kind=%s target=%s", plan.ID, plan.Kind, qualified)
		var statement string
		switch plan.Kind {
		case DDLTruncate:
			statement = "TRUNCATE TABLE " + qualified
		case DDLDropColumn:
			statement = "ALTER TABLE " + qualified + " DROP COLUMN " + quoteDoris(action.OldColumn)
		case DDLRenameColumn:
			statement = "ALTER TABLE " + qualified + " RENAME COLUMN " + quoteDoris(action.OldColumn) + " " + quoteDoris(action.NewColumn)
		}
		if statement != "" {
			// 仅首次登记意图时提交语句。响应丢失或发送前停止时，
			// 后续运行凭原回执核对结果，避免重复提交破坏性 DDL。
			_, submissionErr = d.db.ExecContext(ctx, statement)
		}
	}

	switch plan.Kind {
	case DDLTruncate:
		err = d.reconcileDorisTruncate(ctx, plan, qualified, *receipt)
	case DDLDropColumn, DDLRenameColumn, DDLWidenInteger:
		err = d.reconcileDorisSchemaMutation(ctx, plan, qualified, receipt)
	case DDLAddColumns:
		err = d.applyDorisAddColumns(ctx, plan, qualified, action.Additions, receipt)
	}
	if err != nil {
		if submissionErr != nil {
			return herror.WrapMsg(errors.Join(submissionErr, err), "resolve Doris DDL outcome")
		}
		return err
	}
	receipt.Verified = true
	return d.writeDDLReceipt(ctx, plan.ID, *receipt)
}

// validateDorisDDL 校验 Doris DDL 计划标识和目标结构变更。
func validateDorisDDL(plan DDLPlan) (dorisDDLAction, error) {
	if !dorisLabel.MatchString(plan.ID) {
		return dorisDDLAction{}, herror.NewError("invalid Doris DDL plan identity")
	}
	return inspectDorisDDL(plan)
}

// inspectDorisDDL 按计划类型检查审批要求和结构兼容性，并解析适用的操作。
func inspectDorisDDL(plan DDLPlan) (dorisDDLAction, error) {
	if plan.Before.Database != plan.After.Database || plan.Before.Name != plan.After.Name {
		return dorisDDLAction{}, herror.NewError("Doris DDL plan changes its target table")
	}
	if _, _, err := splitDorisTable(plan.Before.Database + "." + plan.Before.Name); err != nil {
		return dorisDDLAction{}, err
	}
	for _, table := range []DorisTable{plan.Before, plan.After} {
		if _, _, err := dorisTableDefinitions(table); err != nil {
			return dorisDDLAction{}, err
		}
	}
	if !slices.Equal(plan.Before.PrimaryKey, plan.After.PrimaryKey) {
		return dorisDDLAction{}, herror.NewError("Doris DDL plan changes the target primary key")
	}
	if plan.Kind != DDLAddColumns && !plan.RequiresApproval {
		return dorisDDLAction{}, herror.NewError("Doris DDL plan requires an approval barrier")
	}
	switch plan.Kind {
	case DDLTruncate, DDLWidenInteger:
		if !slices.Equal(plan.Before.Columns, plan.After.Columns) {
			return dorisDDLAction{}, herror.NewError("Doris DDL operation requires an unchanged target schema")
		}
		return dorisDDLAction{}, nil
	case DDLAddColumns:
		additions, err := dorisAddedColumns(plan)
		return dorisDDLAction{Additions: additions}, err
	case DDLDropColumn, DDLRenameColumn:
		return inspectDorisValueColumnChange(plan)
	default:
		return dorisDDLAction{}, herror.NewError("unsupported Doris DDL plan")
	}
}

// dorisAddedColumns 检查加列计划并返回新增的可空值列。
func dorisAddedColumns(plan DDLPlan) ([]DorisColumn, error) {
	before := make(map[string]DorisColumn, len(plan.Before.Columns))
	for _, column := range plan.Before.Columns {
		before[column.Name] = column
	}
	var additions []DorisColumn
	for _, column := range plan.After.Columns {
		if old, exists := before[column.Name]; exists {
			if old != column {
				return nil, herror.NewError("Doris DDL plan modifies an existing value column")
			}
			delete(before, column.Name)
		} else {
			if !column.Nullable {
				return nil, herror.NewError("Doris DDL supports adding nullable value columns")
			}
			additions = append(additions, column)
		}
	}
	if len(before) != 0 {
		return nil, herror.NewError("Doris DDL plan removes existing columns")
	}
	// 已校验的源端 ADD COLUMN IF NOT EXISTS 可能保留原有结构。
	// 此时目标只需校验当前结构。
	return additions, nil
}

// inspectDorisValueColumnChange 检查删列或重命名计划涉及的单个非键值列。
func inspectDorisValueColumnChange(plan DDLPlan) (dorisDDLAction, error) {
	before, after := plan.Before.Columns, plan.After.Columns
	drop := plan.Kind == DDLDropColumn
	if (drop && len(before) != len(after)+1) || (!drop && len(before) != len(after)) {
		return dorisDDLAction{}, herror.NewError("Doris DDL plan must change exactly one value column")
	}
	// 未受变更影响的列须保持相对顺序，确保审批绑定到源端记录的操作。
	index := 0
	for index < len(after) && before[index] == after[index] {
		index++
	}
	if index >= len(before) || slices.ContainsFunc(plan.Before.PrimaryKey, func(key string) bool {
		return strings.EqualFold(key, before[index].Name)
	}) {
		return dorisDDLAction{}, herror.NewError("Doris DDL plan must change exactly one non-key value column")
	}
	original := before[index]
	if canonicalDorisDDLType(original.Type) == "BITMAP" {
		return dorisDDLAction{}, herror.NewError("Doris DDL plan changes a Bitmap value column")
	}
	action := dorisDDLAction{OldColumn: original.Name}
	if drop {
		if !slices.Equal(before[index+1:], after[index:]) {
			return dorisDDLAction{}, herror.NewError("Doris DROP COLUMN plan changes existing columns")
		}
		return action, nil
	}
	renamed := after[index]
	if strings.EqualFold(original.Name, renamed.Name) || original.Type != renamed.Type ||
		original.Nullable != renamed.Nullable || !slices.Equal(before[index+1:], after[index+1:]) {
		return dorisDDLAction{}, herror.NewError("Doris RENAME COLUMN plan must preserve the value column definition")
	}
	action.NewColumn = renamed.Name
	return action, nil
}

// ensureDDLReceipts 创建存放 DDL 意图和确认结果的控制表。
func (d *DorisTarget) ensureDDLReceipts(ctx context.Context) error {
	query := "CREATE TABLE IF NOT EXISTS " + d.controlTable("ddl_receipts") + " (" +
		"ddl_id VARCHAR(128) NOT NULL, plan_digest CHAR(64) NOT NULL, " +
		"job_fence BIGINT NOT NULL, partition_ids STRING NOT NULL, verified BOOLEAN NOT NULL) " +
		"ENGINE=OLAP UNIQUE KEY(ddl_id) DISTRIBUTED BY HASH(ddl_id) BUCKETS 1" + dorisUniqueProperties
	if _, err := d.db.ExecContext(ctx, query); err != nil {
		return wrapDorisRequestError(err, "create Doris DDL receipt table")
	}
	return nil
}

// readDDLReceipt 读取指定 DDL 的持久化回执，并核对计划摘要。
func (d *DorisTarget) readDDLReceipt(ctx context.Context, id, checksum string) (*dorisDDLReceipt, error) {
	var receipt dorisDDLReceipt
	var partitions string
	query := "SELECT plan_digest, job_fence, partition_ids, verified FROM " + d.controlTable("ddl_receipts") + " WHERE ddl_id = ?"
	err := d.db.QueryRowContext(ctx, query, id).Scan(&receipt.PlanDigest, &receipt.JobFence, &partitions, &receipt.Verified)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, wrapDorisRequestError(err, "read Doris DDL receipt")
	}
	if receipt.PlanDigest != checksum {
		return nil, herror.NewError("Doris DDL receipt is bound to a different plan")
	}
	if strings.HasPrefix(partitions, "{") {
		var witness dorisDDLJobWitness
		if err := json.Unmarshal([]byte(partitions), &witness); err != nil {
			return nil, herror.WrapMsg(err, "decode Doris DDL job witness")
		}
		if witness.SchemaJobID <= receipt.JobFence || witness.JobDeadline < 0 {
			return nil, herror.NewError("Doris DDL job witness is outside the original job fence")
		}
		receipt.PartitionIDs, receipt.SchemaJobID = witness.PartitionIDs, witness.SchemaJobID
		receipt.JobDeadline = witness.JobDeadline
	} else if err := json.Unmarshal([]byte(partitions), &receipt.PartitionIDs); err != nil {
		return nil, herror.WrapMsg(err, "decode Doris DDL partition witness")
	}
	return &receipt, nil
}

// writeDDLReceipt 写入 DDL 回执，并读回校验涉及的作业和分区。
func (d *DorisTarget) writeDDLReceipt(ctx context.Context, id string, receipt dorisDDLReceipt) error {
	var witness any = receipt.PartitionIDs
	if receipt.SchemaJobID != 0 {
		witness = dorisDDLJobWitness{PartitionIDs: receipt.PartitionIDs, SchemaJobID: receipt.SchemaJobID, JobDeadline: receipt.JobDeadline}
	}
	partitions, err := json.Marshal(witness)
	if err != nil {
		return herror.Wrap(err)
	}
	query := "INSERT INTO " + d.controlTable("ddl_receipts") + " (ddl_id, plan_digest, job_fence, partition_ids, verified) VALUES (?, ?, ?, ?, ?)"
	_, writeErr := d.db.ExecContext(ctx, query, id, receipt.PlanDigest, receipt.JobFence, string(partitions), receipt.Verified)
	actual, readErr := d.readDDLReceipt(ctx, id, receipt.PlanDigest)
	if readErr == nil && actual != nil && actual.JobFence == receipt.JobFence && actual.SchemaJobID == receipt.SchemaJobID &&
		actual.JobDeadline == receipt.JobDeadline && actual.Verified == receipt.Verified && slices.Equal(actual.PartitionIDs, receipt.PartitionIDs) {
		return nil
	}
	if readErr != nil && !errors.Is(readErr, errDorisTargetUnavailable) {
		return herror.WrapMsg(readErr, "verify Doris DDL receipt")
	}
	if writeErr != nil {
		return wrapDorisRequestError(writeErr, "write Doris DDL receipt")
	}
	if readErr != nil {
		return readErr
	}
	return herror.NewError("Doris DDL receipt was not verified")
}

// verifyDorisUniqueTable 核对 Doris 已有目标表是否使用 UNIQUE KEY 模型。
func (d *DorisTarget) verifyDorisUniqueTable(ctx context.Context, table string) error {
	var name, createSQL string
	if err := d.db.QueryRowContext(ctx, "SHOW CREATE TABLE "+table).Scan(&name, &createSQL); err != nil {
		return wrapDorisRequestError(err, "read Doris table model")
	}
	for _, line := range strings.Split(strings.ToUpper(createSQL), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "UNIQUE KEY(") || strings.HasPrefix(line, "UNIQUE KEY (") {
			return nil
		}
	}
	return herror.NewError("Doris target requires an existing UNIQUE KEY table")
}

// canonicalDorisDDLType 归一化 Doris 类型名称和别名以便比较表结构。
func canonicalDorisDDLType(typ string) string {
	typ = strings.ToUpper(strings.Join(strings.Fields(typ), ""))
	switch typ {
	case "TEXT", "VARCHAR(*)":
		return "STRING"
	case "DATEV2":
		return "DATE"
	case "DATETIMEV2", "DATETIMEV2(0)", "DATETIME(0)":
		return "DATETIME"
	case "JSONB":
		return "JSON"
	}
	typ = strings.Replace(typ, "DATETIMEV2(", "DATETIME(", 1)
	return strings.Replace(typ, "DECIMALV3(", "DECIMAL(", 1)
}

// dorisSchemaMatches 判断 Doris 当前表结构是否与预期定义一致。
func (d *DorisTarget) dorisSchemaMatches(ctx context.Context, table DorisTable) (bool, error) {
	difference, err := d.dorisSchemaDifference(ctx, table)
	return difference == "" && err == nil, err
}

// dorisSchemaDifference 读取 Doris 表结构并报告发现的首项差异。
func (d *DorisTarget) dorisSchemaDifference(ctx context.Context, table DorisTable) (string, error) {
	query := "DESC " + quoteDoris(table.Database) + "." + quoteDoris(table.Name)
	rows, err := d.db.QueryContext(ctx, query)
	if err != nil {
		return "", wrapDorisRequestError(err, "describe Doris target schema")
	}
	defer rows.Close()
	fields, err := dorisMetadataFields(rows, "field", "type", "null", "key", "default")
	if err != nil {
		return "", err
	}
	expected := make(map[string]DorisColumn, len(table.Columns))
	for _, column := range table.Columns {
		expected[column.Name] = column
	}
	var keys []string
	var rowCount int
	for rows.Next() {
		if err := fields.scan(rows); err != nil {
			return "", err
		}
		columnName := fields.get("field")
		column, ok := expected[columnName]
		if !ok {
			return fmt.Sprintf("unexpected column %q", columnName), nil
		}
		actualType := fields.get("type")
		actualNullable := strings.EqualFold(fields.get("null"), "yes")
		hasDefault := fields.values[fields.fields["default"]].Valid
		expectedType := canonicalDorisDDLType(column.Type)
		// CREATE TABLE 省略默认值时，Doris 仍会为 BITMAP 列安装空位图默认值。
		// Doris 的 BITMAP 列仅支持这一默认值。
		if canonicalDorisDDLType(actualType) != expectedType || actualNullable != column.Nullable || (hasDefault && expectedType != "BITMAP") {
			return fmt.Sprintf("column %q reported type=%s nullable=%t has_default=%t, expected type=%s nullable=%t", columnName,
				actualType, actualNullable, hasDefault, column.Type, column.Nullable), nil
		}
		delete(expected, columnName)
		if key := fields.get("key"); strings.EqualFold(key, "true") || strings.EqualFold(key, "yes") {
			keys = append(keys, columnName)
		}
		rowCount++
	}
	if err := rows.Err(); err != nil {
		return "", wrapDorisRequestError(err, "read Doris target schema")
	}
	if rowCount != len(table.Columns) {
		return fmt.Sprintf("expected %d columns, found %d", len(table.Columns), rowCount), nil
	}
	if !slices.EqualFunc(keys, table.PrimaryKey, strings.EqualFold) {
		return fmt.Sprintf("expected primary key %v, found %v", table.PrimaryKey, keys), nil
	}
	return "", nil
}

// dorisSchemaJobs 查询目标表的结构作业，统计仍在运行和在原作业边界后出现的记录。
func (d *DorisTarget) dorisSchemaJobs(ctx context.Context, table DorisTable, fence uint64) (dorisSchemaJobStatus, error) {
	query := "SHOW ALTER TABLE COLUMN FROM " + quoteDoris(table.Database) + " WHERE TableName = ?"
	rows, err := d.db.QueryContext(ctx, query, table.Name)
	if err != nil {
		return dorisSchemaJobStatus{}, wrapDorisRequestError(err, "read Doris schema change jobs")
	}
	defer rows.Close()
	fields, err := dorisMetadataFields(rows, "jobid", "state")
	if err != nil {
		return dorisSchemaJobStatus{}, err
	}
	var status dorisSchemaJobStatus
	seen := make(map[uint64]string)
	for rows.Next() {
		if err := fields.scan(rows); err != nil {
			return dorisSchemaJobStatus{}, err
		}
		id, err := strconv.ParseUint(fields.get("jobid"), 10, 64)
		if err != nil || id == 0 {
			return dorisSchemaJobStatus{}, herror.NewError("invalid Doris schema change job ID")
		}
		state := strings.ToUpper(fields.get("state"))
		// 一个作业可能为每个物化索引返回一行，作业状态须一致。
		if previous, exists := seen[id]; exists {
			if previous != state {
				return dorisSchemaJobStatus{}, herror.NewError("Doris returned conflicting states for one schema change job")
			}
			continue
		}
		seen[id] = state
		if id >= status.Latest.ID {
			status.Latest = dorisSchemaJob{ID: id, State: state}
		}
		if id > fence {
			status.AfterFence++
		}
		if state != "FINISHED" && state != "CANCELLED" {
			status.Active++
		}
	}
	if err := rows.Err(); err != nil {
		return dorisSchemaJobStatus{}, wrapDorisRequestError(err, "read Doris schema change jobs")
	}
	return status, nil
}

// verifyDorisSchemaJob 绑定唯一可归属当前计划的运行作业，后续只核对原作业。
func (d *DorisTarget) verifyDorisSchemaJob(ctx context.Context, id string, receipt *dorisDDLReceipt, jobs dorisSchemaJobStatus) error {
	job := jobs.Latest
	if jobs.AfterFence > 1 || (receipt.SchemaJobID != 0 && (jobs.AfterFence != 1 || job.ID != receipt.SchemaJobID)) {
		return herror.NewError("Doris schema change jobs conflict with the original DDL receipt")
	}
	if jobs.Active == 0 {
		return nil
	}
	if jobs.Active != 1 || jobs.AfterFence != 1 {
		return herror.NewError("Doris active schema job cannot be attributed to the original DDL receipt")
	}
	switch job.State {
	case "PENDING", "WAITING_TXN", "RUNNING":
	default:
		return herror.NewError("Doris schema change job has an unexpected active state")
	}
	if receipt.SchemaJobID == 0 || receipt.JobDeadline == 0 {
		timeout := d.config.DDLJobTimeout
		if timeout == 0 {
			timeout = defaultDorisDDLJobTimeout
		}
		receipt.SchemaJobID = job.ID
		receipt.JobDeadline = time.Now().Add(timeout).UnixMilli()
		if err := d.writeDDLReceipt(ctx, id, *receipt); err != nil {
			return err
		}
	}
	if time.Now().UnixMilli() >= receipt.JobDeadline {
		return herror.NewError(fmt.Sprintf("Doris DDL %s schema job %d exceeded its persisted monitoring deadline", id, job.ID))
	}
	return herror.Wrap(fmt.Errorf("Doris DDL %s schema job %d is %s: %w", id, job.ID, job.State, error(ErrLoadPending)))
}

// applyDorisAddColumns 执行并确认兼容的加列操作，在前置条件成立时重试。
func (d *DorisTarget) applyDorisAddColumns(ctx context.Context, plan DDLPlan, table string, additions []DorisColumn, receipt *dorisDDLReceipt) error {
	parts := make([]string, 0, len(additions))
	for _, column := range additions {
		parts = append(parts, quoteDoris(column.Name)+" "+strings.ToUpper(strings.Join(strings.Fields(column.Type), ""))+" NULL")
	}
	statement := "ALTER TABLE " + table + " ADD COLUMN (" + strings.Join(parts, ", ") + ")"
	var submitted bool
	var executeErr error
	for {
		jobs, err := d.dorisSchemaJobs(ctx, plan.Before, receipt.JobFence)
		if err != nil {
			return err
		}
		if err := d.verifyDorisSchemaJob(ctx, plan.ID, receipt, jobs); err != nil {
			return err
		}
		job := jobs.Latest
		if job.ID > receipt.JobFence && job.State == "CANCELLED" {
			return herror.NewError("Doris schema change was cancelled before installing the expected schema")
		}
		matches, err := d.dorisSchemaMatches(ctx, plan.After)
		if err != nil {
			return err
		}
		if matches {
			return nil
		}
		if receipt.SchemaJobID != 0 || jobs.AfterFence != 0 {
			return herror.NewError("Doris original ADD COLUMN job did not install the expected schema")
		}
		before, err := d.dorisSchemaMatches(ctx, plan.Before)
		if err != nil {
			return err
		}
		if !before {
			return herror.NewError("Doris schema differs from both sides of the ADD COLUMN plan")
		}
		if len(additions) == 0 {
			return herror.NewError("Doris ADD COLUMN no-op could not verify its schema")
		}
		if !submitted {
			// ADD COLUMN 属于非破坏性操作。发生中断后，若前置条件依然成立，
			// 可使用相同的列名和类型重试。
			submitted = true
			if _, executeErr = d.db.ExecContext(ctx, statement); executeErr != nil {
				executeErr = herror.WrapMsg(executeErr, "submit Doris ADD COLUMN")
			}
		}
		if err := waitForDorisDDL(ctx); err != nil {
			return herror.WrapMsg(errors.Join(executeErr, err), "wait for expected Doris schema after ADD COLUMN")
		}
	}
}

// reconcileDorisSchemaMutation 根据原回执、分区和结构变更作业核对目标结构是否已变更。
func (d *DorisTarget) reconcileDorisSchemaMutation(ctx context.Context, plan DDLPlan, table string, receipt *dorisDDLReceipt) error {
	if plan.Kind != DDLWidenInteger && len(receipt.PartitionIDs) == 0 {
		return herror.NewError("Doris schema mutation is missing its original partition witness")
	}
	for {
		jobs, err := d.dorisSchemaJobs(ctx, plan.Before, receipt.JobFence)
		if err != nil {
			return err
		}
		if plan.Kind == DDLWidenInteger && (jobs.Active != 0 || jobs.AfterFence != 0) {
			return herror.NewError("Doris integer widening unexpectedly observed a target schema change job")
		}
		if err := d.verifyDorisSchemaJob(ctx, plan.ID, receipt, jobs); err != nil {
			return err
		}
		job := jobs.Latest
		if job.ID > receipt.JobFence && job.State == "CANCELLED" {
			return herror.NewError("Doris cancelled the schema mutation after its intent was stored")
		}
		if plan.Kind != DDLWidenInteger {
			partitions, err := d.dorisPartitionIDs(ctx, table)
			if err != nil {
				return err
			}
			if !slices.Equal(partitions, receipt.PartitionIDs) {
				return herror.NewError("Doris schema mutation target changed its partition identity")
			}
		}
		difference, err := d.dorisSchemaDifference(ctx, plan.After)
		if err != nil {
			return err
		}
		if difference == "" {
			return nil
		}
		if plan.Kind == DDLWidenInteger {
			return herror.NewError("Doris integer widening requires an unchanged target schema: " + difference)
		}
		before, err := d.dorisSchemaMatches(ctx, plan.Before)
		if err != nil {
			return err
		}
		if !before {
			return herror.NewError("Doris schema differs from both sides of the approved mutation: " + difference)
		}
		if receipt.SchemaJobID != 0 || jobs.AfterFence != 0 {
			return herror.NewError("Doris original DDL job did not install the expected schema: " + difference)
		}
		if err := waitForDorisDDL(ctx); err != nil {
			return herror.Wrap(fmt.Errorf("Doris %s outcome is uncertain; verify whether the original request completed: %w", plan.Kind, err))
		}
	}
}

// dorisPartitionIDs 读取并排序目标表的分区标识，供 DDL 结果确认。
func (d *DorisTarget) dorisPartitionIDs(ctx context.Context, table string) ([]uint64, error) {
	rows, err := d.db.QueryContext(ctx, "SHOW PARTITIONS FROM "+table)
	if err != nil {
		return nil, wrapDorisRequestError(err, "read Doris partition identity")
	}
	defer rows.Close()
	fields, err := dorisMetadataFields(rows, "partitionid")
	if err != nil {
		return nil, err
	}
	var result []uint64
	for rows.Next() {
		if err := fields.scan(rows); err != nil {
			return nil, err
		}
		id, err := strconv.ParseUint(fields.get("partitionid"), 10, 64)
		if err != nil || id == 0 {
			return nil, herror.NewError("invalid Doris partition ID")
		}
		result = append(result, id)
	}
	if err := rows.Err(); err != nil {
		return nil, wrapDorisRequestError(err, "read Doris partition IDs")
	}
	if len(result) == 0 {
		return nil, herror.NewError("Doris DDL requires at least one verifiable partition")
	}
	slices.Sort(result)
	return result, nil
}

// reconcileDorisTruncate 依据原分区标识变化和空表校验确认清表结果。
func (d *DorisTarget) reconcileDorisTruncate(ctx context.Context, plan DDLPlan, table string, receipt dorisDDLReceipt) error {
	if len(receipt.PartitionIDs) == 0 {
		return herror.NewError("Doris TRUNCATE receipt is missing the original partition identity")
	}
	for {
		matches, err := d.dorisSchemaMatches(ctx, plan.After)
		if err != nil {
			return err
		}
		if !matches {
			return herror.NewError("Doris TRUNCATE target schema moved out of the expected version")
		}
		after, err := d.dorisPartitionIDs(ctx, table)
		if err != nil {
			return err
		}
		if !slices.Equal(after, receipt.PartitionIDs) {
			if len(after) != len(receipt.PartitionIDs) {
				return herror.NewError("Doris TRUNCATE target partition count changed")
			}
			for _, id := range receipt.PartitionIDs {
				if slices.Contains(after, id) {
					return herror.NewError("Doris TRUNCATE replaced only some of the target partitions")
				}
			}
			if err := d.VerifyEmptyTable(ctx, plan.After.Database, plan.After.Name); err != nil {
				return wrapDorisRequestError(err, "verify truncated Doris target table")
			}
			return nil
		}
		if err := waitForDorisDDL(ctx); err != nil {
			return herror.WrapMsg(err, "Doris TRUNCATE outcome is uncertain; original partition IDs are still present")
		}
	}
}

// waitForDorisDDL 在两次 DDL 状态查询之间短暂等待，并响应上下文取消。
func waitForDorisDDL(ctx context.Context) error {
	timer := time.NewTimer(150 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type dorisSQLResult struct {
	fields       map[string]int
	values       []sql.NullString
	destinations []any
}

// dorisMetadataFields 按列名定位查询结果中必需的 Doris 元数据字段。
func dorisMetadataFields(rows *sql.Rows, required ...string) (*dorisSQLResult, error) {
	names, err := rows.Columns()
	if err != nil {
		return nil, herror.WrapMsg(err, "read Doris metadata columns")
	}
	result := &dorisSQLResult{
		fields:       make(map[string]int, len(names)),
		values:       make([]sql.NullString, len(names)),
		destinations: make([]any, len(names)),
	}
	for i, name := range names {
		result.fields[strings.ToLower(name)] = i
		result.destinations[i] = &result.values[i]
	}
	for _, name := range required {
		if _, exists := result.fields[name]; !exists {
			return nil, herror.NewError(fmt.Sprintf("Doris metadata column %q is missing", name))
		}
	}
	return result, nil
}

// scan 将当前 Doris 元数据行读入字段缓冲。
func (r *dorisSQLResult) scan(rows *sql.Rows) error {
	if err := rows.Scan(r.destinations...); err != nil {
		return herror.WrapMsg(err, "scan Doris metadata")
	}
	return nil
}

// get 按名称取得已读入的 Doris 元数据字段值。
func (r *dorisSQLResult) get(name string) string {
	return r.values[r.fields[name]].String
}
