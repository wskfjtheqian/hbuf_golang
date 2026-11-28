package hcdc

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
)

// ErrDorisLoadOutcomeUnknown 表示确认或重新提交导入所需的证据不足。
// 原请求意图会保留，供后续核对。
var ErrDorisLoadOutcomeUnknown = herror.NewError("Doris load outcome is unknown")

const (
	dorisDeleteColumn          = "__cdc_delete"
	dorisLoadRecheckTimeout    = 15 * time.Second
	maxDorisLoadRecheckTimeout = 10 * time.Minute
	dorisLoadRecheckPeriod     = time.Second
)

type dorisLoadPayload struct {
	Columns string
	CSV     string
}

// DorisRow 保存一条完整的目标行。nil 值对应 SQL NULL。
type DorisRow struct {
	Values []*string
	Delete bool
}

// EncodeDorisLoad 将源数据编码为稳定的 Stream Load 请求内容。
// Base64 编码保留逗号、换行、空字符串和 NULL 标记字面量。
func EncodeDorisLoad(columns []string, rows []DorisRow) ([]byte, error) {
	values := make([]DorisRow, len(rows))
	aliases := make([]string, len(columns))
	expressions := make([]string, len(columns))
	for index, column := range columns {
		if !dorisIdentifier.MatchString(column) {
			return nil, herror.NewError("invalid Doris destination column: " + column)
		}
		aliases[index] = fmt.Sprintf("__cdc_value_%d", index)
		expressions[index] = quoteDoris(column) + "=if(" + aliases[index] + "='-', '', from_base64(" + aliases[index] + "))"
	}
	for index, row := range rows {
		values[index].Delete = row.Delete
		values[index].Values = make([]*string, len(row.Values))
		for field, value := range row.Values {
			if value != nil {
				encoded := "-"
				if *value != "" {
					encoded = base64.StdEncoding.EncodeToString([]byte(*value))
				}
				values[index].Values[field] = &encoded
			}
		}
	}
	return EncodeDorisLoadWithExpressions(aliases, expressions, values)
}

// EncodeDorisLoadWithExpressions 编码可安全写入 CSV 的字段，并将列映射与数据一并保存，
// 以便原样重放。表达式须由可信的源结构映射器构造。
func EncodeDorisLoadWithExpressions(columns, expressions []string, rows []DorisRow) ([]byte, error) {
	if len(columns) == 0 || len(rows) == 0 {
		return nil, herror.NewError("Doris load requires columns and rows")
	}
	mappings := make([]string, 0, len(columns)+len(expressions)+1)
	names := make(map[string]struct{}, len(columns))
	for _, name := range columns {
		folded := strings.ToLower(name)
		if _, exists := names[folded]; !dorisIdentifier.MatchString(name) || folded == dorisDeleteColumn || exists {
			return nil, herror.NewError("invalid or duplicate Doris CSV column: " + name)
		}
		names[folded] = struct{}{}
		mappings = append(mappings, quoteDoris(name))
	}
	mappings = append(mappings, dorisDeleteColumn)
	for _, expression := range expressions {
		if strings.TrimSpace(expression) == "" || strings.ContainsAny(expression, "\r\n\x00") {
			return nil, herror.NewError("invalid Doris CSV mapping expression")
		}
		mappings = append(mappings, expression)
	}
	var csv strings.Builder
	for _, row := range rows {
		if len(row.Values) != len(columns) {
			return nil, herror.NewError("Doris CSV row has an unexpected field count")
		}
		for _, value := range row.Values {
			if value == nil {
				csv.WriteString(`\N`)
			} else {
				if *value == `\N` || strings.ContainsAny(*value, ",\r\n\x00") {
					return nil, herror.NewError("Doris CSV value must be encoded before loading")
				}
				csv.WriteString(*value)
			}
			csv.WriteByte(',')
		}
		if row.Delete {
			csv.WriteString("1\n")
		} else {
			csv.WriteString("0\n")
		}
	}
	payload, err := json.Marshal(dorisLoadPayload{Columns: strings.Join(mappings, ","), CSV: csv.String()})
	if err != nil {
		return nil, herror.Wrap(err)
	}
	return payload, nil
}

type dorisReceipt struct {
	TargetTable string
	Checksum    string
	Rows        uint64
	Verified    bool
}

// validateDorisBatch 校验 Doris 导入批次的目标、Label、行数和校验和。
func validateDorisBatch(batch LoadBatch) error {
	if !dorisLabel.MatchString(batch.Label) || batch.Checksum != digest(batch.Payload) ||
		batch.RowCount == 0 || batch.RowCount > math.MaxInt64 {
		return herror.NewError("Doris batch has an invalid label, checksum or row count")
	}
	_, _, err := splitDorisTable(batch.TargetTable)
	return err
}

// loadReceipt 读取指定批次的持久回执，并核对目标、校验和和行数。
func (d *DorisTarget) loadReceipt(ctx context.Context, batch LoadBatch) (*dorisReceipt, error) {
	var receipt dorisReceipt
	query := "SELECT target_table, payload_checksum, expected_rows, verified FROM " + d.controlTable("load_receipts") +
		" WHERE load_label = ?"
	err := d.db.QueryRowContext(ctx, query, batch.Label).Scan(
		&receipt.TargetTable, &receipt.Checksum, &receipt.Rows, &receipt.Verified,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, wrapDorisRequestError(err, "read Doris load receipt")
	}
	if receipt.TargetTable != batch.TargetTable || receipt.Checksum != batch.Checksum || receipt.Rows != batch.RowCount {
		return nil, herror.NewError("Doris label is already bound to a different load request")
	}
	return &receipt, nil
}

// persistReceipt 写入 Doris 导入回执，并读回校验其内容和确认状态。
func (d *DorisTarget) persistReceipt(ctx context.Context, batch LoadBatch, verified bool) error {
	_, writeErr := d.db.ExecContext(ctx, "INSERT INTO "+d.controlTable("load_receipts")+
		" (load_label, target_table, payload_checksum, expected_rows, verified) VALUES (?, ?, ?, ?, ?)",
		batch.Label, batch.TargetTable, batch.Checksum, batch.RowCount, verified)
	receipt, readErr := d.loadReceipt(ctx, batch)
	if readErr == nil && receipt != nil && receipt.Verified == verified {
		return nil
	}
	if readErr != nil && !errors.Is(readErr, errDorisTargetUnavailable) {
		return herror.WrapMsg(readErr, "verify Doris load receipt")
	}
	if writeErr != nil {
		return wrapDorisRequestError(writeErr, "write Doris load receipt")
	}
	if readErr != nil {
		return readErr
	}
	return herror.NewError("Doris load receipt could not be verified")
}

// dorisConfirmed 构造与原批次绑定的成功导入确认。
func dorisConfirmed(batch LoadBatch) LoadConfirmation {
	return LoadConfirmation{
		State: LoadSucceeded, Label: batch.Label, Checksum: batch.Checksum, LoadedRows: batch.RowCount,
	}
}

// ConfirmLoad 根据持久化回执确认对应的导入。
// 先前的 PUT 结果不明时，查询 FE 事务状态和 BE 导入审计。
func (d *DorisTarget) ConfirmLoad(ctx context.Context, batch LoadBatch) (LoadConfirmation, error) {
	if err := validateDorisBatch(batch); err != nil {
		return LoadConfirmation{}, err
	}
	receipt, err := d.loadReceipt(ctx, batch)
	if err != nil {
		return LoadConfirmation{}, err
	}
	if receipt == nil {
		// 每次发送前都会持久化与该 Label 绑定的回执。
		return LoadConfirmation{State: LoadAbsent}, nil
	}
	if receipt.Verified {
		return dorisConfirmed(batch), nil
	}
	return d.resolveDorisLoad(ctx, batch)
}

// SubmitLoad 在发起 Stream Load 之前登记不可变的导入意图。
// 已登记意图的结果不明时，先确认原请求结果再决定后续处理。
func (d *DorisTarget) SubmitLoad(ctx context.Context, batch LoadBatch) (LoadConfirmation, error) {
	if err := validateDorisBatch(batch); err != nil {
		return LoadConfirmation{}, err
	}
	existing, err := d.loadReceipt(ctx, batch)
	if err != nil {
		return LoadConfirmation{}, err
	}
	if existing != nil {
		if existing.Verified {
			return dorisConfirmed(batch), nil
		}
		return d.resolveDorisLoad(ctx, batch)
	}

	database, table, _ := splitDorisTable(batch.TargetTable)
	var payload dorisLoadPayload
	if err := json.Unmarshal(batch.Payload, &payload); err != nil {
		return LoadConfirmation{}, herror.WrapMsg(err, "decode Doris Stream Load payload")
	}
	if payload.Columns == "" || strings.ContainsAny(payload.Columns, "\r\n\x00") ||
		uint64(strings.Count(payload.CSV, "\n")) != batch.RowCount {
		return LoadConfirmation{}, herror.NewError("Doris Stream Load payload columns or row count are invalid")
	}
	endpoint := d.backend.JoinPath("api", database, table, "_stream_load")
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint.String(), strings.NewReader(payload.CSV))
	if err != nil {
		return LoadConfirmation{}, herror.WrapMsg(err, "build Doris Stream Load request")
	}
	request.Header.Set("Expect", "100-continue")
	request.Header.Set("label", batch.Label)
	request.Header.Set("comment", batch.Checksum)
	request.Header.Set("format", "csv")
	request.Header.Set("column_separator", ",")
	request.Header.Set("columns", payload.Columns)
	request.Header.Set("merge_type", "MERGE")
	request.Header.Set("delete", dorisDeleteColumn+"=1")
	request.Header.Set("strict_mode", "true")
	request.Header.Set("max_filter_ratio", "0")
	request.Header.Set("group_commit", "off_mode")
	if err := d.trackLoadReceipt(ctx, batch.Label); err != nil {
		return LoadConfirmation{}, err
	}
	if err := d.persistReceipt(ctx, batch, false); err != nil {
		return LoadConfirmation{}, err
	}

	hlog.Debug(ctx, "Doris Stream Load table=%s load_label=%s expected_rows=%d", batch.TargetTable, batch.Label, batch.RowCount)
	var response struct {
		TxnID                uint64
		Status               string
		Label                string
		Comment              string
		NumberTotalRows      *uint64
		NumberLoadedRows     *uint64
		NumberFilteredRows   *uint64
		NumberUnselectedRows *uint64
	}
	if err := d.requestJSON(request, &response); err != nil {
		var invalidJSON *json.SyntaxError
		if errors.As(err, &invalidJSON) {
			err = errors.Join(ErrLoadPending, err)
		}
		return LoadConfirmation{}, herror.WrapMsg(errors.Join(ErrDorisLoadOutcomeUnknown, err), "Doris Stream Load response")
	}
	switch response.Status {
	case "Success":
		if response.Label != batch.Label || response.Comment != batch.Checksum ||
			response.NumberTotalRows == nil || *response.NumberTotalRows != batch.RowCount ||
			response.NumberLoadedRows == nil || *response.NumberLoadedRows != batch.RowCount ||
			response.NumberFilteredRows == nil || *response.NumberFilteredRows != 0 ||
			response.NumberUnselectedRows == nil || *response.NumberUnselectedRows != 0 {
			return LoadConfirmation{}, herror.NewError(fmt.Sprintf("Doris Stream Load response did not match load_label=%s expected_rows=%d total_rows=%s loaded_rows=%s filtered_rows=%s unselected_rows=%s",
				batch.Label, batch.RowCount, dorisReportedRows(response.NumberTotalRows), dorisReportedRows(response.NumberLoadedRows),
				dorisReportedRows(response.NumberFilteredRows), dorisReportedRows(response.NumberUnselectedRows)))
		}
		if response.TxnID == 0 {
			return LoadConfirmation{}, errors.Join(ErrLoadPending, ErrDorisLoadOutcomeUnknown, herror.NewError("Doris Stream Load success omitted its transaction ID"))
		}
		if err := d.verifyDorisLoadTransaction(ctx, database, batch.Label, response.TxnID); err != nil {
			return LoadConfirmation{}, err
		}
		if err := d.persistReceipt(ctx, batch, true); err != nil {
			return LoadConfirmation{}, err
		}
		return dorisConfirmed(batch), nil
	case "Publish Timeout", "Label Already Exists":
		return d.resolveDorisLoad(ctx, batch)
	case "":
		return LoadConfirmation{}, errors.Join(ErrLoadPending, ErrDorisLoadOutcomeUnknown, herror.NewError("Doris Stream Load response omitted its status"))
	default:
		return LoadConfirmation{}, herror.NewError("Doris Stream Load rejected the batch")
	}
}

// dorisReportedRows 将 Doris 返回的行数格式化为报错信息，区分缺失值。
func dorisReportedRows(rows *uint64) string {
	if rows == nil {
		return "missing"
	}
	return strconv.FormatUint(*rows, 10)
}

// loadState 按数据库和 Label 查询 Doris FE 返回的导入事务状态。
func (d *DorisTarget) loadState(ctx context.Context, database, label string) (string, error) {
	endpoint := d.frontend.JoinPath("api", database, "get_load_state")
	endpoint.RawQuery = url.Values{"label": {label}}.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return "", herror.WrapMsg(err, "build Doris get load state request")
	}
	var response struct {
		Code *int
		Data string
	}
	if err := d.requestJSON(request, &response); err != nil {
		return "", err
	}
	if response.Code == nil || *response.Code != 0 {
		return "", ErrDorisLoadOutcomeUnknown
	}
	return response.Data, nil
}

// verifyHTTPAccess 查询 FE 状态以验证 HTTP 凭据，通常会收到测试 Label 的 UNKNOWN 状态。
// 整个过程只读取目标状态。
func (d *DorisTarget) verifyHTTPAccess(ctx context.Context, database string) error {
	state, err := d.loadState(ctx, database, "cdc_config_probe")
	if err != nil {
		// 传输错误可能包含带凭据的 Location 响应头。
		switch {
		case errors.Is(err, context.DeadlineExceeded):
			return herror.Wrap(fmt.Errorf("Doris HTTP access probe: %w", context.DeadlineExceeded))
		case errors.Is(err, context.Canceled):
			return herror.Wrap(fmt.Errorf("Doris HTTP access probe: %w", context.Canceled))
		default:
			return herror.NewError("Doris HTTP access probe failed")
		}
	}
	switch state {
	case "UNKNOWN", "PREPARE", "PRECOMMITTED", "COMMITTED", "VISIBLE", "ABORTED":
		return nil
	default:
		return herror.NewError("Doris HTTP access probe returned an invalid load state")
	}
}

// resolveDorisLoad 在原 Label 的持久截止时间内重查导入证据。截止后每次显式恢复只核对一次。
func (d *DorisTarget) resolveDorisLoad(ctx context.Context, batch LoadBatch) (LoadConfirmation, error) {
	database, table, _ := splitDorisTable(batch.TargetTable)
	lookupCtx, cancelLookup := context.WithTimeout(ctx, dorisLoadRecheckTimeout)
	deadline, err := d.dorisLoadRecheckDeadline(lookupCtx, batch.Label)
	cancelLookup()
	if err != nil {
		return LoadConfirmation{}, err
	}
	expired := !time.Now().Before(deadline)
	if expired {
		deadline = time.Now().Add(dorisLoadRecheckTimeout)
	}
	probeCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	for {
		confirmation, unresolved, err := d.probeDorisLoad(probeCtx, database, table, batch)
		if err != nil && probeCtx.Err() != nil && errors.Is(err, probeCtx.Err()) {
			return LoadConfirmation{}, dorisLoadRecheckError(ctx, batch.Label)
		}
		if err != nil || !unresolved {
			return confirmation, err
		}
		if expired {
			return LoadConfirmation{}, dorisLoadRecheckError(ctx, batch.Label)
		}
		timer := time.NewTimer(dorisLoadRecheckPeriod)
		select {
		case <-probeCtx.Done():
			timer.Stop()
			return LoadConfirmation{}, dorisLoadRecheckError(ctx, batch.Label)
		case <-timer.C:
		}
	}
}

// dorisLoadRecheckError 报告核对预算耗尽或调用方取消，保留原 Label 供处置。
func dorisLoadRecheckError(ctx context.Context, label string) error {
	if err := ctx.Err(); err != nil {
		return herror.Wrap(err)
	}
	return herror.Wrap(fmt.Errorf("Doris load recheck deadline elapsed for label %s: %w", label, error(ErrDorisLoadOutcomeUnknown)))
}

// dorisLoadRecheckDeadline 读取或登记该 Label 首次核对的截止时间，重启沿用原值。
func (d *DorisTarget) dorisLoadRecheckDeadline(ctx context.Context, label string) (time.Time, error) {
	table := d.controlTable("load_recheck_deadlines")
	query := "SELECT deadline_ms FROM " + table + " WHERE load_label = ?"
	var deadline int64
	err := d.db.QueryRowContext(ctx, query, label).Scan(&deadline)
	if !errors.Is(err, sql.ErrNoRows) {
		if err != nil {
			return time.Time{}, wrapDorisRequestError(err, "read Doris load recheck deadline")
		}
		if deadline <= 0 || deadline > time.Now().Add(maxDorisLoadRecheckTimeout).UnixMilli() {
			return time.Time{}, herror.NewError("Doris load recheck deadline is invalid")
		}
		return time.UnixMilli(deadline), nil
	}

	timeout := d.config.LoadRecheckTimeout
	if timeout == 0 {
		timeout = dorisLoadRecheckTimeout
	}
	expected := time.Now().Add(timeout).UnixMilli()
	_, writeErr := d.db.ExecContext(ctx, "INSERT INTO "+table+" (load_label, deadline_ms) VALUES (?, ?)", label, expected)
	readErr := d.db.QueryRowContext(ctx, query, label).Scan(&deadline)
	if readErr == nil && deadline == expected {
		return time.UnixMilli(deadline), nil
	}
	if err := errors.Join(writeErr, readErr); err != nil {
		return time.Time{}, wrapDorisRequestError(err, "persist and verify Doris load recheck deadline")
	}
	return time.Time{}, herror.NewError("Doris load recheck deadline did not match its original value")
}

// probeDorisLoad 核对一次原 Label。只有 FE 状态未知或审计尚未出现时继续等待。
func (d *DorisTarget) probeDorisLoad(ctx context.Context, database, table string, batch LoadBatch) (LoadConfirmation, bool, error) {
	state, err := d.loadState(ctx, database, batch.Label)
	if err != nil {
		return LoadConfirmation{}, false, err
	}
	switch state {
	case "PREPARE", "PRECOMMITTED", "COMMITTED":
		return LoadConfirmation{State: LoadPending, Label: batch.Label, Checksum: batch.Checksum}, false, nil
	case "VISIBLE":
		found, err := d.auditDorisLoad(ctx, database, table, batch)
		if err != nil || !found {
			return LoadConfirmation{}, !found && err == nil, err
		}
		if err := d.verifyDorisLoadTransaction(ctx, database, batch.Label, 0); err != nil {
			return LoadConfirmation{}, false, err
		}
		if err := d.persistReceipt(ctx, batch, true); err != nil {
			return LoadConfirmation{}, false, err
		}
		return dorisConfirmed(batch), false, nil
	case "ABORTED":
		return LoadConfirmation{}, false, herror.NewError("Doris load was aborted; inspect its audit before recovery")
	case "UNKNOWN":
		// UNKNOWN 可能表示事务从未发起，也可能表示事务 Label 已过期。
		_, err := d.auditDorisLoad(ctx, database, table, batch)
		return LoadConfirmation{}, err == nil, err
	default:
		return LoadConfirmation{}, false, herror.Wrap(fmt.Errorf("unexpected Doris load state %q: %w", state, error(ErrDorisLoadOutcomeUnknown)))
	}
}

// verifyDorisLoadTransaction 通过 Doris SQL 确认指定导入事务已经可见。
func (d *DorisTarget) verifyDorisLoadTransaction(ctx context.Context, database, label string, id uint64) error {
	where := "LABEL = '" + label + "'"
	if id != 0 {
		where = "ID = " + strconv.FormatUint(id, 10)
	}
	rows, err := d.db.QueryContext(ctx, "SHOW TRANSACTION FROM "+quoteDoris(database)+" WHERE "+where)
	if err != nil {
		return wrapDorisRequestError(errors.Join(ErrDorisLoadOutcomeUnknown, err), "query Doris SQL transaction")
	}
	defer rows.Close()
	fields, err := dorisMetadataFields(rows, "transactionid", "label", "transactionstatus")
	if err != nil {
		return herror.WrapMsg(errors.Join(ErrDorisLoadOutcomeUnknown, err), "read Doris SQL transaction columns")
	}
	if rows.Next() {
		if err := fields.scan(rows); err != nil {
			return herror.WrapMsg(errors.Join(ErrDorisLoadOutcomeUnknown, err), "read Doris SQL transaction")
		}
		actualID, err := strconv.ParseUint(fields.get("transactionid"), 10, 64)
		matched := err == nil && actualID > 0 && (id == 0 || id == actualID) && fields.get("label") == label &&
			strings.EqualFold(fields.get("transactionstatus"), "VISIBLE")
		if matched && !rows.Next() && rows.Err() == nil {
			return nil
		}
	}
	if err := rows.Err(); err != nil {
		return wrapDorisRequestError(errors.Join(ErrDorisLoadOutcomeUnknown, err), "iterate Doris SQL transaction")
	}
	return errors.Join(ErrDorisLoadOutcomeUnknown, herror.NewError(fmt.Sprintf("Doris SQL connection did not confirm visible transaction load_label=%s transaction_id=%d", label, id)))
}

// requestJSON 发送带 Doris 凭据的 HTTP 请求并解析 JSON 响应。
func (d *DorisTarget) requestJSON(request *http.Request, result any) error {
	request.SetBasicAuth(d.config.Username, d.config.Password)
	response, err := d.client.Do(request)
	if err != nil {
		return wrapDorisRequestError(err, "request Doris HTTP endpoint")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		err := herror.NewError(fmt.Sprintf("Doris HTTP status %d", response.StatusCode))
		switch response.StatusCode {
		case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			return errors.Join(errDorisTargetUnavailable, err)
		default:
			return err
		}
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	if err := decoder.Decode(result); err != nil {
		return wrapDorisRequestError(err, "decode Doris HTTP response")
	}
	return nil
}

// auditDorisLoad 在 BE 导入审计中查找并校验与原批次匹配的记录。
func (d *DorisTarget) auditDorisLoad(ctx context.Context, database, table string, batch LoadBatch) (bool, error) {
	query := "SHOW STREAM LOAD FROM " + quoteDoris(database) + " WHERE LABEL = '" + batch.Label + "'"
	rows, err := d.db.QueryContext(ctx, query)
	if err != nil {
		return false, wrapDorisRequestError(err, "query Doris Stream Load audit")
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return false, herror.WrapMsg(err, "read Doris Stream Load audit fields")
	}
	pointers := make([]any, len(columns))
	values := make([]sql.NullString, len(columns))
	for index := range values {
		pointers[index] = &values[index]
	}
	found := false
	for rows.Next() {
		if err := rows.Scan(pointers...); err != nil {
			return false, herror.WrapMsg(err, "read Doris Stream Load audit")
		}
		fields := make(map[string]string, len(columns))
		for index, name := range columns {
			fields[strings.ToLower(strings.ReplaceAll(name, "_", ""))] = values[index].String
		}
		status := fields["status"]
		expected := strconv.FormatUint(batch.RowCount, 10)
		if fields["label"] != batch.Label || !strings.EqualFold(fields["table"], table) ||
			(!strings.EqualFold(status, "Success") && !strings.EqualFold(status, "Publish Timeout")) ||
			fields["comment"] != batch.Checksum || fields["loadedrows"] != expected || fields["totalrows"] != expected ||
			fields["filteredrows"] != "0" || fields["unselectedrows"] != "0" {
			return false, herror.NewError(fmt.Sprintf("Doris Stream Load audit did not match load_label=%s table=%q status=%q expected_rows=%d total_rows=%q loaded_rows=%q filtered_rows=%q unselected_rows=%q",
				batch.Label, fields["table"], status, batch.RowCount, fields["totalrows"], fields["loadedrows"], fields["filteredrows"], fields["unselectedrows"]))
		}
		found = true
	}
	if err := rows.Err(); err != nil {
		return false, wrapDorisRequestError(err, "iterate Doris Stream Load audit")
	}
	return found, nil
}
