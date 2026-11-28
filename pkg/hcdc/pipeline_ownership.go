package cdc

import (
	"strings"
	"sync"

	"github.com/go-sql-driver/mysql"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
)

type pipelineOwnership struct {
	targetEndpoint  string
	targetDatabase  string
	controlDatabase string
	targetIdentity  StreamIdentity
	sourceAddress   string
	replicationID   uint32
}

var openPipelines = struct {
	sync.Mutex
	owners map[pipelineOwnership]struct{}
}{owners: make(map[pipelineOwnership]struct{})}

// pipelineOwner 从配置提取同进程单写检查所需的源、目标和检查点所有者标识。
func pipelineOwner(config PipelineConfig) (pipelineOwnership, error) {
	dsn, err := mysql.ParseDSN(config.Doris.SQLDSN)
	if err != nil {
		return pipelineOwnership{}, herror.NewError("invalid Doris SQL DSN")
	}
	control := config.Doris.ControlDatabase
	if control == "" {
		control = defaultDorisControlDatabase
	}
	return pipelineOwnership{
		targetEndpoint:  dsn.Net + "/" + strings.ToLower(dsn.Addr),
		targetDatabase:  strings.ToLower(config.TargetDatabase),
		controlDatabase: strings.ToLower(control),
		targetIdentity:  config.Identity,
		sourceAddress:   strings.ToLower(config.Source.Address),
		replicationID:   config.Source.ServerID,
	}, nil
}

// conflict 检查复制客户端 ID、目标库、检查点和源标识是否与另一个所有者冲突。
func (owner pipelineOwnership) conflict(active pipelineOwnership) error {
	if owner.sourceAddress == active.sourceAddress && owner.replicationID == active.replicationID {
		return herror.NewError("CDC replication server ID is already active for the source endpoint")
	}
	if owner.targetEndpoint == active.targetEndpoint && (owner.targetDatabase == active.targetDatabase || owner.targetDatabase == active.controlDatabase ||
		owner.controlDatabase == active.targetDatabase ||
		(owner.controlDatabase == active.controlDatabase && owner.targetIdentity == active.targetIdentity)) {
		return herror.NewError("CDC target database or checkpoint identity is already owned by another pipeline")
	}
	if owner.targetIdentity.SourceID == active.targetIdentity.SourceID {
		return herror.NewError("CDC source ID is already active")
	}
	return nil
}

// acquirePipelineOwnership 注册当前流水线的所有权，并返回退出时使用的释放函数。
func acquirePipelineOwnership(config PipelineConfig) (func(), error) {
	owner, err := pipelineOwner(config)
	if err != nil {
		return nil, err
	}
	openPipelines.Lock()
	defer openPipelines.Unlock()
	for active := range openPipelines.owners {
		if err := owner.conflict(active); err != nil {
			return nil, err
		}
	}
	openPipelines.owners[owner] = struct{}{}
	return func() {
		openPipelines.Lock()
		defer openPipelines.Unlock()
		delete(openPipelines.owners, owner)
	}, nil
}
