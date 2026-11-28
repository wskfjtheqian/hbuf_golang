package cdc

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sync"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc/internal/mysqlsource"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
)

// Manager 管理独立的源流水线，并按源应用配置变更。使用 NewManager 创建实例。
type Manager struct {
	ctx     context.Context
	updates chan struct{}
	mu      sync.RWMutex
	sources map[string]managedSource
}

type managedConfig struct {
	config   PipelineConfig
	manifest bootstrapManifest
	owner    pipelineOwnership
}

type managedSource struct {
	config   managedConfig
	pipeline *Pipeline
}

// NewManager 创建管理器，并用 ctx 约束受管流水线的运行生命周期。
// 每次 SetConfig 使用独立的上下文等待变更完成。
func NewManager(ctx context.Context) *Manager {
	return &Manager{
		ctx: ctx, updates: make(chan struct{}, 1), sources: make(map[string]managedSource),
	}
}

// Pipeline 返回指定源的当前句柄。该源更换配置后可再次查询新句柄。
func (m *Manager) Pipeline(sourceID string) (*Pipeline, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	source, ok := m.sources[sourceID]
	return source.pipeline, ok
}

// SetConfig 应用完整的期望配置。传入 nil 或空配置会移除全部源。
// 配置等价且受管源均处于活动状态时直接返回。
// 已完成快照的同代次更新先验证候选，失败时恢复原配置。超时后恢复可在后台完成。
// 新增源和新代次沿用 StartPipeline 的异步启动方式。
func (m *Manager) SetConfig(ctx context.Context, config *Config) (resultErr error) {
	select {
	case m.updates <- struct{}{}:
	case <-ctx.Done():
		return herror.Wrap(ctx.Err())
	}
	// 恢复记录保存原配置和必须先释放所有权的最后一个流水线句柄。
	var recovery map[string]managedSource
	var candidates map[string]*Pipeline
	defer func() {
		for _, pipeline := range candidates {
			pipeline.cancel()
		}
		resultErr = errors.Join(resultErr, m.finishConfigUpdate(recovery))
	}()
	if err := ctx.Err(); err != nil {
		return herror.Wrap(err)
	}
	if config != nil && len(config.Pipelines) != 0 {
		if err := m.ctx.Err(); err != nil {
			return herror.Wrap(err)
		}
	}
	m.mu.RLock()
	current := maps.Clone(m.sources)
	m.mu.RUnlock()
	if matchesAppliedConfig(config, current) {
		return nil
	}
	desired, err := prepareManagerConfig(config, current)
	if err != nil {
		return err
	}

	var retiring []string
	for _, id := range slices.Sorted(maps.Keys(current)) {
		source := current[id]
		next, keep := desired[id]
		state := source.pipeline.Status().State
		if keep {
			if equalPipelineConfig(source.config.config, next.config) && !configRestartRequired(state) {
				delete(desired, id)
				continue
			}
			if state == PipelineStarting {
				return herror.NewError("CDC source " + id + " is starting; wait for startup before replacing its configuration")
			}
		}
		retiring = append(retiring, id)
	}

	recovery = make(map[string]managedSource)
	candidates = make(map[string]*Pipeline)
	for _, id := range retiring {
		if err := ctx.Err(); err != nil {
			return herror.Wrap(err)
		}
		source := current[id]
		pipeline := source.pipeline
		if next, keep := desired[id]; keep && next.config.Identity == source.config.config.Identity &&
			pipeline.Status().Progress.Delivered.Identity == next.config.Identity {
			recovery[id] = source
			// 更新期限约束调用方的等待，在途交付继续使用原请求期限。
			pipeline.requestStop()
			select {
			case <-pipeline.Done():
			case <-ctx.Done():
				return herror.Wrap(fmt.Errorf("drain CDC source %q for configuration update: %w", id, ctx.Err()))
			}
			continue
		}
		stopErr := pipeline.Close(ctx)
		select {
		case <-pipeline.Done():
			// 原句柄保留终止错误，Done 表示目录和连接所有权已释放。
		default:
			return herror.Wrap(fmt.Errorf("stop CDC source %q: %w", id, stopErr))
		}
		if _, replace := desired[id]; !replace {
			m.mu.Lock()
			delete(m.sources, id)
			m.mu.Unlock()
			hlog.Info(ctx, "CDC source removed source_id=%s", id)
		}
	}

	// 等待所有同代次候选就绪后统一启用。即使候选配置互换复制 ID，失败时也能恢复原所有权。
	activate := make(chan struct{})
	for _, id := range slices.Sorted(maps.Keys(recovery)) {
		if err := ctx.Err(); err != nil {
			return herror.Wrap(err)
		}
		next := desired[id]
		pipeline, err := startPipeline(m.ctx, next.config, pipelineStartOptions{resumeOnly: true, activate: activate})
		if err != nil {
			return herror.Wrap(fmt.Errorf("start CDC config candidate for source %q: %w", id, err))
		}
		candidates[id] = pipeline
		previous := recovery[id]
		previous.pipeline = pipeline
		recovery[id] = previous
		if err := pipeline.waitReady(ctx); err != nil {
			return herror.Wrap(fmt.Errorf("verify CDC config candidate for source %q: %w", id, err))
		}
	}
	if err := ctx.Err(); err != nil {
		return herror.Wrap(err)
	}
	m.mu.Lock()
	for id, pipeline := range candidates {
		m.sources[id] = managedSource{config: desired[id], pipeline: pipeline}
		delete(desired, id)
	}
	close(activate)
	m.mu.Unlock()
	for id := range candidates {
		hlog.Info(ctx, "CDC source configuration updated source_id=%s", id)
	}
	clear(candidates)
	clear(recovery)

	for _, id := range slices.Sorted(maps.Keys(desired)) {
		if err := ctx.Err(); err != nil {
			return herror.Wrap(err)
		}
		next := desired[id]
		pipeline, err := StartPipeline(m.ctx, next.config)
		if err != nil {
			return herror.Wrap(fmt.Errorf("start CDC source %q: %w", id, err))
		}
		m.mu.Lock()
		m.sources[id] = managedSource{config: next, pipeline: pipeline}
		m.mu.Unlock()
		hlog.Info(ctx, "CDC source configured source_id=%s generation=%s", id, next.config.Identity.Generation)
	}
	return nil
}

// matchesAppliedConfig 比较期望配置与当前实际应用的配置，并检查受管源仍处于活动状态。
func matchesAppliedConfig(config *Config, current map[string]managedSource) bool {
	if config == nil {
		return len(current) == 0
	}
	if len(config.Pipelines) != len(current) {
		return false
	}
	applied := Config{Pipelines: make([]PipelineConfig, 0, len(current))}
	for _, source := range current {
		if configRestartRequired(source.pipeline.Status().State) {
			return false
		}
		applied.Pipelines = append(applied.Pipelines, source.config.config)
	}
	return config.Equal(&applied)
}

// configRestartRequired 判断流水线的状态是否需要重新启动以应用期望配置。
func configRestartRequired(state PipelineState) bool {
	return state == PipelineFailed || state == PipelineStopped || state == PipelineStopping
}

// finishConfigUpdate 在恢复记录都可交接时完成恢复，否则将更新锁交给后台恢复。
// 后续 SetConfig 等待当前交接释放更新锁。
func (m *Manager) finishConfigUpdate(recovery map[string]managedSource) error {
	for _, source := range recovery {
		select {
		case <-source.pipeline.Done():
		default:
			go func() {
				if err := m.restorePreviousConfigs(recovery); err != nil {
					herror.PrintStack(m.ctx, err)
				}
			}()
			return nil
		}
	}
	return m.restorePreviousConfigs(recovery)
}

// restorePreviousConfigs 等待退出实例释放资源，用原配置续传并释放更新锁。
func (m *Manager) restorePreviousConfigs(recovery map[string]managedSource) (resultErr error) {
	defer func() { <-m.updates }()
	for _, source := range recovery {
		select {
		case <-source.pipeline.Done():
		case <-m.ctx.Done():
			return nil
		}
	}
	for _, id := range slices.Sorted(maps.Keys(recovery)) {
		if m.ctx.Err() != nil {
			break
		}
		previous := recovery[id]
		pipeline, err := startPipeline(m.ctx, previous.config.config, pipelineStartOptions{resumeOnly: true})
		if err != nil {
			resultErr = errors.Join(resultErr, herror.Wrap(fmt.Errorf("restore previous CDC config for source %q: %w", id, err)))
			continue
		}
		m.mu.Lock()
		m.sources[id] = managedSource{config: previous.config, pipeline: pipeline}
		m.mu.Unlock()
		hlog.Info(m.ctx, "CDC source restoring previous configuration source_id=%s", id)
	}
	return resultErr
}

// prepareManagerConfig 在交接前校验整份期望配置及其与当前所有者的资源冲突。
func prepareManagerConfig(config *Config, current map[string]managedSource) (map[string]managedConfig, error) {
	desired, err := prepareDesiredConfig(config)
	if err != nil || len(desired) == 0 {
		return desired, err
	}
	for id, next := range desired {
		for previousID, source := range current {
			old := source.config
			if id == previousID && old.config.Identity.Generation == next.config.Identity.Generation {
				if err := validateManagedRestart(old, next); err != nil {
					return nil, herror.Wrap(fmt.Errorf("update CDC source %q: %w", id, err))
				}
				continue
			}
			if old.config.Directory == next.config.Directory ||
				(old.owner.targetEndpoint == next.owner.targetEndpoint && old.owner.targetDatabase == next.owner.targetDatabase) {
				return nil, herror.NewError("CDC source " + id + " needs an independent buffer directory and Doris target for a new stream")
			}
		}
	}

	openPipelines.Lock()
	defer openPipelines.Unlock()
	for active := range openPipelines.owners {
		if source, managed := current[active.targetIdentity.SourceID]; managed && active == source.config.owner {
			select {
			case <-source.pipeline.Done():
			default:
				continue
			}
		}
		for id, next := range desired {
			if err := next.owner.conflict(active); err != nil {
				return nil, herror.Wrap(fmt.Errorf("CDC source %q conflicts with an active pipeline: %w", id, err))
			}
		}
	}
	return desired, nil
}

// prepareManagedConfig 校验单源配置，归一化目录、表顺序与默认值并记录资源所有权。
func prepareManagedConfig(config PipelineConfig) (managedConfig, error) {
	manifest, _, err := validatePipelineConfig(config)
	if err != nil {
		return managedConfig{}, err
	}
	if _, _, err := mysqlsource.ParseAddress(config.Source.Address); err != nil {
		return managedConfig{}, err
	}
	if _, _, _, err := parseDorisConfig(config.Doris); err != nil {
		return managedConfig{}, err
	}
	directory, err := resolvePipelineDirectory(config.Directory)
	if err != nil {
		return managedConfig{}, err
	}
	owner, err := pipelineOwner(config)
	if err != nil {
		return managedConfig{}, err
	}
	config.Directory = directory
	config.Source.Tables = manifest.SourceTables
	return managedConfig{config: withPipelineConfigDefaults(config), manifest: manifest, owner: owner}, nil
}

// validateManagedRestart 核对同代次续传所需的流映射、目录、Doris 绑定及物理源地址。
func validateManagedRestart(old, next managedConfig) error {
	if !reflect.DeepEqual(old.manifest, next.manifest) ||
		old.config.Directory != next.config.Directory || old.owner.targetEndpoint != next.owner.targetEndpoint ||
		old.config.Doris.ControlDatabase != next.config.Doris.ControlDatabase ||
		old.config.Doris.FrontendURL != next.config.Doris.FrontendURL || old.config.Doris.StreamLoadURL != next.config.Doris.StreamLoadURL {
		return herror.NewError("changing the CDC stream mapping, buffer directory or Doris binding requires a new generation")
	}
	if !old.manifest.GTIDAutoPosition && old.config.Source.Address != next.config.Source.Address {
		return herror.NewError("changing the physical CDC source address requires a new generation")
	}
	return nil
}

// resolvePipelineDirectory 解析已存在的祖先目录，使符号链接下的新缓冲目录在创建前后使用同一路径标识。
func resolvePipelineDirectory(dir string) (string, error) {
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return "", herror.WrapMsg(err, "resolve CDC buffer directory")
	}
	for parent := absolute; ; parent = filepath.Dir(parent) {
		resolved, err := filepath.EvalSymlinks(parent)
		if err == nil {
			info, err := os.Stat(resolved)
			if err != nil {
				return "", herror.WrapMsg(err, "inspect CDC buffer directory")
			}
			if !info.IsDir() {
				return "", herror.NewError("CDC buffer path is not a directory")
			}
			rest, err := filepath.Rel(parent, absolute)
			if err != nil {
				return "", herror.WrapMsg(err, "resolve CDC buffer directory")
			}
			return filepath.Join(resolved, rest), nil
		}
		if !errors.Is(err, fs.ErrNotExist) || parent == filepath.Dir(parent) {
			return "", herror.WrapMsg(err, "resolve CDC buffer directory")
		}
	}
}
