package cdc

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"

	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
)

// Config 描述管理器期望运行的全部同步源。
type Config struct {
	Pipelines []PipelineConfig `yaml:"Pipelines"` // 全部受管流水线的配置。
}

// ZeroDatePolicy 指定遇到 MySQL 或 MariaDB 零日期时的处理方式。
type ZeroDatePolicy string

const (
	// ZeroDateReject 阻塞含零日期的记录，是省略配置时的默认值。
	ZeroDateReject ZeroDatePolicy = "reject"
	// ZeroDateEpoch 沿用 hcdc，将零日期映射为 1970-01-01 并保留时间部分。
	ZeroDateEpoch ZeroDatePolicy = "epoch"
)

// Validate 校验各源的配置和资源冲突，并记录校验失败的原因。连接检查由流水线启动时执行。
func (c *Config) Validate(ctx context.Context) bool {
	if c == nil {
		hlog.Error(ctx, "CDC 配置为空")
		return false
	}
	if _, err := prepareDesiredConfig(c); err != nil {
		hlog.Error(ctx, "CDC 配置校验失败: %v", err)
		return false
	}
	return true
}

// Equal 按 SourceID 比较两份配置。源和选表顺序无关，两个 nil 相等。
func (c *Config) Equal(other *Config) bool {
	if c == nil || other == nil {
		return c == other
	}
	if len(c.Pipelines) != len(other.Pipelines) {
		return false
	}
	unmatched := make(map[string]PipelineConfig, len(c.Pipelines))
	for _, pipeline := range c.Pipelines {
		unmatched[pipeline.Identity.SourceID] = pipeline
	}
	if len(unmatched) != len(c.Pipelines) {
		return false
	}
	for _, pipeline := range other.Pipelines {
		id := pipeline.Identity.SourceID
		previous, ok := unmatched[id]
		if !ok || !equalPipelineConfig(previous, pipeline) {
			return false
		}
		delete(unmatched, id)
	}
	return true
}

// equalPipelineConfig 比较单源的连接、映射和运行配置，兼容选表重排和显式默认值。
func equalPipelineConfig(a, b PipelineConfig) bool {
	a, b = withPipelineConfigDefaults(a), withPipelineConfigDefaults(b)
	if a.Directory != b.Directory {
		if strings.TrimSpace(a.Directory) == "" || strings.TrimSpace(b.Directory) == "" {
			return false
		}
		var aErr, bErr error
		a.Directory, aErr = resolvePipelineDirectory(a.Directory)
		b.Directory, bErr = resolvePipelineDirectory(b.Directory)
		if aErr != nil || bErr != nil {
			return false
		}
	}
	aTables, bTables := a.Source.Tables, b.Source.Tables
	a.Source.Tables, b.Source.Tables = nil, nil
	return reflect.DeepEqual(a, b) && (slices.Equal(aTables, bTables) ||
		slices.Equal(slices.Sorted(slices.Values(aTables)), slices.Sorted(slices.Values(bTables))))
}

// withPipelineConfigDefaults 补齐用于配置比较和续传校验的默认值。
func withPipelineConfigDefaults(config PipelineConfig) PipelineConfig {
	if config.Source.Flavor == "" {
		config.Source.Flavor = "mysql"
	}
	if config.Doris.ControlDatabase == "" {
		config.Doris.ControlDatabase = defaultDorisControlDatabase
	}
	if config.Doris.ControlRetentionRecords == 0 {
		config.Doris.ControlRetentionRecords = defaultControlRetentionRecords
	}
	if config.Doris.DDLJobTimeout == 0 {
		config.Doris.DDLJobTimeout = defaultDorisDDLJobTimeout
	}
	if config.Doris.LoadRecheckTimeout == 0 {
		config.Doris.LoadRecheckTimeout = dorisLoadRecheckTimeout
	}
	if config.ZeroDatePolicy == "" {
		config.ZeroDatePolicy = ZeroDateReject
	}
	if config.MaxPendingBytes == 0 {
		config.MaxPendingBytes = defaultPendingBytes
	}
	if config.MaxTransactionBytes == 0 {
		config.MaxTransactionBytes = config.MaxPendingBytes
	}
	return config
}

// prepareDesiredConfig 校验完整的期望配置，并按源整理供应用阶段使用的配置。
func prepareDesiredConfig(config *Config) (map[string]managedConfig, error) {
	desired := make(map[string]managedConfig)
	if config == nil {
		return desired, nil
	}
	for _, pipeline := range config.Pipelines {
		id := pipeline.Identity.SourceID
		if _, duplicate := desired[id]; duplicate {
			return nil, herror.NewError("duplicate CDC source ID " + id)
		}
		next, err := prepareManagedConfig(pipeline)
		if err != nil {
			return nil, herror.Wrap(fmt.Errorf("validate CDC source %q: %w", id, err))
		}
		for _, earlier := range desired {
			if err := next.owner.conflict(earlier.owner); err != nil {
				return nil, herror.Wrap(fmt.Errorf("conflicting CDC sources %q and %q: %w", earlier.config.Identity.SourceID, id, err))
			}
			if next.config.Directory == earlier.config.Directory {
				return nil, herror.NewError("CDC sources " + earlier.config.Identity.SourceID + " and " + id + " share a buffer directory")
			}
		}
		desired[id] = next
	}
	return desired, nil
}
