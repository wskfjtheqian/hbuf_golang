package hcdc

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc/internal/mysqlsource"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc/internal/spool"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
)

type tableStream struct {
	pipeline *Pipeline
	err      error
}

// beginTableWrite 将单表快照或增量交付纳入库流水线的暂停控制。
func (p *Pipeline) beginTableWrite(ctx context.Context) (func(), error) {
	if p == nil {
		return nil, nil
	}
	for {
		p.mu.Lock()
		if err := ctx.Err(); err != nil || p.stopping {
			p.mu.Unlock()
			if err == nil {
				err = context.Canceled
			}
			return nil, herror.Wrap(err)
		}
		if p.pausedReason == "" && p.pendingControl == nil && p.controlWrite == nil {
			p.tableWrites++
			p.mu.Unlock()
			return func() {
				p.mu.Lock()
				p.tableWrites--
				if p.tableWrites == 0 && p.tableWritesDone != nil {
					close(p.tableWritesDone)
					p.tableWritesDone = nil
				}
				p.mu.Unlock()
			}, nil
		}
		changed := p.changed
		p.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return nil, herror.Wrap(ctx.Err())
		}
	}
}

// superviseTables 恢复已登记的单表流，随后查询和登记新表。在父流释放写入权前关闭全部子流。
func (p *Pipeline) superviseTables(ctx, scanCtx context.Context, source *mysqlsource.Source, config pipelineConfig, owner dorisWriterIdentity, activate <-chan struct{}) {
	if activate != nil {
		select {
		case <-activate:
		case <-scanCtx.Done():
			return
		}
	}
	if err := p.waitReady(scanCtx); err != nil {
		return
	}
	store, err := spool.Open(filepath.Join(config.Directory, "table-enrollments"))
	if err != nil {
		p.reportTableDiscovery(scanCtx, err)
		return
	}
	defer store.Close()
	enrolled, known, err := readTableEnrollments(store, config)
	if err != nil {
		p.reportTableDiscovery(scanCtx, err)
		return
	}

	children := make(map[string]*Pipeline)
	var observers sync.WaitGroup
	defer func() {
		for _, child := range children {
			child.requestStop()
		}
		for _, child := range children {
			<-child.Done()
		}
		observers.Wait()
	}()

	var pending string
	timer := time.NewTimer(config.tableDiscovery.Interval)
	defer timer.Stop()
	for scanCtx.Err() == nil {
		for _, table := range enrolled {
			if children[table] != nil || scanCtx.Err() != nil {
				continue
			}
			childConfig := tablePipelineConfig(config, table)
			child, err := startTablePipeline(ctx, childConfig, owner, p, source.Identity())
			p.recordTableStream(table, child, err)
			if err != nil {
				continue
			}
			children[table] = child
			observers.Go(func() {
				_ = child.WatchStatus(scanCtx, func(context.Context, PipelineStatus) {
					p.mu.Lock()
					p.signalStatusLocked()
					p.mu.Unlock()
				})
			})
			hlog.Info(scanCtx, "CDC 已启动自动发现的表 source_id=%s table=%s", config.Identity.SourceID, table)
		}
		select {
		case <-scanCtx.Done():
			return
		case <-timer.C:
		}
		if pending != "" {
			// 目录同步结果未知时，先用原序号和原表名确认登记。
			if err := store.Append(uint64(len(enrolled))+1, []byte(pending)); err != nil {
				p.reportTableDiscovery(scanCtx, err)
				timer.Reset(config.tableDiscovery.Interval)
				continue
			}
			enrolled = append(enrolled, pending)
			known[strings.ToLower(pending)] = pending
			pending = ""
		}
		scan, cancel := context.WithTimeout(scanCtx, canalDiscoveryTimeout)
		names, err := source.DiscoverTables(scan, config.Source.Database)
		cancel()
		if err == nil {
			for _, table := range names {
				if !config.tableDiscovery.Filter.allows(table) || !validPipelineIdentifier(table) || scanCtx.Err() != nil {
					continue
				}
				key := strings.ToLower(table)
				if previous := known[key]; previous != "" {
					if previous != table {
						p.recordTableStream(table, nil, herror.NewError("自动发现表与已有 Doris 目标表同名"))
					}
					continue
				}
				if err = store.Append(uint64(len(enrolled))+1, []byte(table)); err != nil {
					pending = table
					break
				}
				enrolled = append(enrolled, table)
				known[key] = table
			}
		}
		p.reportTableDiscovery(scanCtx, err)
		timer.Reset(config.tableDiscovery.Interval)
	}
}

// readTableEnrollments 核对父代次绑定、连续记录和大小写归一化后的目标表唯一性。
func readTableEnrollments(store *spool.Store, config pipelineConfig) ([]string, map[string]string, error) {
	identity, err := json.Marshal(config.Identity)
	if err != nil {
		return nil, nil, herror.Wrap(err)
	}
	if _, err := store.Bind(identity); err != nil {
		return nil, nil, err
	}
	sequences, err := store.Sequences()
	if err != nil {
		return nil, nil, err
	}
	known := make(map[string]string, len(sequences)+len(config.Source.Tables))
	for _, table := range config.Source.Tables {
		known[strings.ToLower(table)] = table
	}
	enrolled := make([]string, 0, len(sequences))
	for i, sequence := range sequences {
		if sequence != uint64(i)+1 {
			return nil, nil, herror.NewError("CDC table enrollment has a sequence gap")
		}
		payload, err := store.Read(sequence)
		if err != nil {
			return nil, nil, err
		}
		table := string(payload)
		key := strings.ToLower(table)
		if !validPipelineIdentifier(table) || !config.tableDiscovery.Filter.allows(table) || known[key] != "" {
			return nil, nil, herror.NewError("CDC table enrollment conflicts with the configured stream")
		}
		enrolled = append(enrolled, table)
		known[key] = table
	}
	return enrolled, known, nil
}

// tablePipelineConfig 为单表生成稳定的流标识、本地目录和复制客户端 ID。
func tablePipelineConfig(parent pipelineConfig, table string) pipelineConfig {
	config := parent
	config.tableDiscovery = nil
	config.Identity.PipelineID += "-table"
	config.Identity.SourceID += "." + table
	config.Directory = filepath.Join(parent.Directory, "table-streams", table)
	config.Source.Tables = []string{table}
	hash := sha256.Sum256([]byte(dorisStreamKey(config.Identity)))
	config.Source.ServerID = binary.BigEndian.Uint32(hash[:4])
	if config.Source.ServerID == 0 || config.Source.ServerID == parent.Source.ServerID {
		config.Source.ServerID = 1
		if parent.Source.ServerID == 1 {
			config.Source.ServerID = 2
		}
	}
	return config
}

// recordTableStream 发布表的启动句柄或失败信息。
func (p *Pipeline) recordTableStream(name string, pipeline *Pipeline, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.tableStreams == nil {
		p.tableStreams = make(map[string]tableStream)
	}
	previous := p.tableStreams[name]
	if previous.pipeline == pipeline && errorMessage(previous.err) == errorMessage(err) {
		return
	}
	p.tableStreams[name] = tableStream{pipeline: pipeline, err: err}
	p.signalStatusLocked()
}

// reportTableDiscovery 发布表目录或元数据错误，不改变父流的同步状态。
func (p *Pipeline) reportTableDiscovery(ctx context.Context, err error) {
	if ctx.Err() != nil {
		return
	}
	p.mu.Lock()
	message := errorMessage(err)
	changed := p.status.TableDiscoveryError != message
	if changed {
		p.status.TableDiscoveryError = message
		p.signalStatusLocked()
	}
	id := p.status.Identity.SourceID
	p.mu.Unlock()
	if changed && err != nil {
		hlog.Warn(ctx, "CDC 自动发现新表失败 source_id=%s error=%v", id, err)
	}
}

// errorMessage 安全读取可以为空的错误信息。
func errorMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// tableStatusesLocked 逐表提取子流状态，父流进度继续表示原有表。
func (p *Pipeline) tableStatusesLocked() []TableSyncStatus {
	if len(p.tableStreams) == 0 {
		return nil
	}
	out := make([]TableSyncStatus, 0, len(p.tableStreams))
	for name, child := range p.tableStreams {
		status := PipelineStatus{State: PipelineFailed, Err: child.err}
		if child.pipeline != nil {
			status = child.pipeline.Status()
		}
		out = append(out, TableSyncStatus{Name: name, Status: status})
	}
	slices.SortFunc(out, func(a, b TableSyncStatus) int { return strings.Compare(a.Name, b.Name) })
	return out
}

// approveTableDDL 按当前屏障 ID 在子流中定位 DDL 审批。
func (p *Pipeline) approveTableDDL(ctx context.Context, id string) (bool, error) {
	p.mu.Lock()
	var chosen *Pipeline
	if p.status.PendingDDL == nil || p.status.PendingDDL.ID != id {
		for _, child := range p.tableStreams {
			if child.pipeline == nil {
				continue
			}
			if pending := child.pipeline.Status().PendingDDL; pending != nil && pending.ID == id {
				chosen = child.pipeline
				break
			}
		}
	}
	p.mu.Unlock()
	if chosen == nil {
		return false, nil
	}
	return true, chosen.ApproveDDL(ctx, id)
}
