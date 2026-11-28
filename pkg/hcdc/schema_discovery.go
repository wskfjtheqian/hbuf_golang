package hcdc

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
)

// DiscoverSchemas 查找拥有匹配基础表的新源库，已有源 ID 保留原配置。
// 单个源实例不可用或部分候选库冲突时，返回其他可用候选库及对应错误。
func (c *Config) DiscoverSchemas(ctx context.Context, known []ResolvedSource) ([]ResolvedSource, error) {
	doris, selections, err := prepareCanalConfig(c)
	if err != nil {
		return nil, err
	}
	knownIDs := make(map[string]struct{}, len(known))
	for _, source := range known {
		knownIDs[source.Identity().SourceID] = struct{}{}
	}

	results := make([]struct {
		tables map[string][]string
		err    error
	}, len(selections))
	var scans sync.WaitGroup
	for i, selection := range selections {
		scans.Go(func() { results[i].tables, results[i].err = selection.discover(ctx) })
	}
	scans.Wait()
	if err := ctx.Err(); err != nil {
		return nil, herror.Wrap(err)
	}

	var candidates []managedConfig
	var failures []error
	for i, selection := range selections {
		catalog := results[i]
		if catalog.err != nil {
			failures = append(failures, catalog.err)
			continue
		}
		prefix := expandCanalPrefix(c.Doris.SchemaPrefix, selection.config.InstanceId)
		for _, schema := range slices.Sorted(maps.Keys(catalog.tables)) {
			id := selection.schemaSourceID(schema)
			if _, exists := knownIDs[id]; exists {
				continue
			}
			pipeline := selection.schemaPipeline(schema, catalog.tables[schema], doris, prefix)
			candidate, err := prepareManagedConfig(pipeline)
			if err != nil {
				failures = append(failures, herror.Wrap(fmt.Errorf("validate CDC source %q: %w", id, err)))
				continue
			}
			candidates = append(candidates, candidate)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, herror.Wrap(err)
	}
	if len(candidates) == 0 {
		return nil, errors.Join(failures...)
	}

	established := make([]managedConfig, 0, len(known))
	for _, source := range known {
		owner, err := pipelineOwner(source.config)
		if err != nil {
			return nil, errors.Join(append(failures, err)...)
		}
		established = append(established, managedConfig{config: source.config, owner: owner})
	}
	eligible := candidates[:0]
	for _, candidate := range candidates {
		if err := schemaConflicts(candidate, established); err != nil {
			failures = append(failures, err)
			continue
		}
		eligible = append(eligible, candidate)
	}

	// 同一轮扫描发现的库互相冲突时，保留双方等待明确的目标映射。
	blocked := make([]bool, len(eligible))
	for i, previous := range eligible {
		for j := i + 1; j < len(eligible); j++ {
			if err := schemaConflict(eligible[j], previous); err != nil {
				blocked[i], blocked[j] = true, true
				failures = append(failures, err)
			}
		}
	}
	var discovered []ResolvedSource
	for i, candidate := range eligible {
		if !blocked[i] {
			discovered = append(discovered, newResolvedSource(candidate.config))
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, herror.Wrap(err)
	}
	return discovered, errors.Join(failures...)
}

// schemaConflicts 检查新库是否会占用任何已有源的资源。
func schemaConflicts(candidate managedConfig, established []managedConfig) error {
	for _, previous := range established {
		if err := schemaConflict(candidate, previous); err != nil {
			return err
		}
	}
	return nil
}

// schemaConflict 检查两个库的目标、复制 ID 和持久目录是否重叠。
func schemaConflict(candidate, previous managedConfig) error {
	id, earlier := candidate.config.Identity.SourceID, previous.config.Identity.SourceID
	if err := candidate.owner.conflict(previous.owner); err != nil {
		return herror.Wrap(fmt.Errorf("conflicting CDC sources %q and %q: %w", earlier, id, err))
	}
	if candidate.config.Directory == previous.config.Directory {
		return herror.NewError(fmt.Sprintf("CDC sources %q and %q share a buffer directory", earlier, id))
	}
	return nil
}
