package hcdc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
)

type DorisResponse struct {
	TxnId                  int    `json:"TxnId"`
	Label                  string `json:"Label"`
	Comment                string `json:"Comment"`
	TwoPhaseCommit         string `json:"TwoPhaseCommit"`
	Status                 string `json:"Status"`
	Message                string `json:"Message"`
	NumberTotalRows        int    `json:"NumberTotalRows"`
	NumberLoadedRows       int    `json:"NumberLoadedRows"`
	NumberFilteredRows     int    `json:"NumberFilteredRows"`
	NumberUnselectedRows   int    `json:"NumberUnselectedRows"`
	LoadBytes              int    `json:"LoadBytes"`
	LoadTimeMs             int    `json:"LoadTimeMs"`
	BeginTxnTimeMs         int    `json:"BeginTxnTimeMs"`
	StreamLoadPutTimeMs    int    `json:"StreamLoadPutTimeMs"`
	ReadDataTimeMs         int    `json:"ReadDataTimeMs"`
	WriteDataTimeMs        int    `json:"WriteDataTimeMs"`
	ReceiveDataTimeMs      int    `json:"ReceiveDataTimeMs"`
	CommitAndPublishTimeMs int    `json:"CommitAndPublishTimeMs"`
}

type DorisConfig struct {
	LogDir  string
	LoadURL string
}

type Doris struct {
	cfg     *DorisConfig
	workers map[SchemaTable]*Worker
	client  *http.Client
	mu      sync.RWMutex
}

func NewDoris(cfg *DorisConfig) *Doris {
	return &Doris{
		cfg:     cfg,
		workers: make(map[SchemaTable]*Worker),
		client:  &http.Client{Timeout: 60 * time.Second},
	}
}

func (d *Doris) RegisterWorker(schema Schema, table Table) *Worker {
	d.mu.Lock()
	defer d.mu.Unlock()

	key := SchemaTable(string(schema) + "." + string(table))
	w := NewWorker(schema, table, d.cfg.LogDir)
	d.workers[key] = w
	return w
}

func (d *Doris) Start(ctx context.Context) {
	d.mu.RLock()
	for _, worker := range d.workers {
		go worker.loop(ctx)
	}
	d.mu.RUnlock()
	go d.loop(ctx)
}

func (d *Doris) AddData(ctx context.Context, schema Schema, table Table, currentCols []Column, values [][]RawBytes) error {
	d.mu.RLock()
	val, ok := d.workers[SchemaTable(string(schema)+"."+string(table))]
	d.mu.RUnlock()
	if !ok {
		return nil
	}
	return val.AddData(ctx, currentCols, values)
}

func (d *Doris) StreamSave(ctx context.Context, schema Schema, table Table, columns string, value io.Reader) error {
	parse, err := url.Parse(d.cfg.LoadURL)
	if err != nil {
		return err
	}
	parse.Path = fmt.Sprintf("/api/%s/%s_stream_load", schema, table)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, parse.String(), value)
	if err != nil {
		return herror.Wrap(err)
	}

	req.SetBasicAuth("root", "")
	req.Header.Set("Expect", "100-continue")
	req.Header.Set("column_separator", ",")

	// ✨ 核心映射：动态把从文件首行解析出来的 columns 传给 Doris
	req.Header.Set("columns", columns)
	req.Header.Set("merge_type", "MERGE")
	req.Header.Set("delete", "__op=2")

	resp, err := d.client.Do(req)
	if err != nil {
		return herror.Wrap(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return herror.NewError("doris stream load failed: " + resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return herror.Wrap(err)
	}

	var dorisResp DorisResponse
	if err = json.Unmarshal(body, &dorisResp); err != nil {
		return herror.Wrap(err)
	}
	if dorisResp.Status != "Success" {
		return herror.NewError("doris stream load failed: " + dorisResp.Message)
	}
	return nil
}

func (d *Doris) loop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.mu.RLock()
			for _, worker := range d.workers {
				_ = worker.checkAndRotate(ctx, true)

				err := worker.readFile(ctx, func(ctx context.Context, columns string, reader io.Reader) error {
					return d.StreamSave(ctx, worker.schema, worker.table, columns, reader)
				})
				if err != nil {
					herror.PrintStack(ctx, err)
				}
			}
			d.mu.RUnlock()
		}
	}
}
