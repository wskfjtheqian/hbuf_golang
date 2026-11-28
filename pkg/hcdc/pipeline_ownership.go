package hcdc

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
func pipelineOwner(config pipelineConfig) (pipelineOwnership, error) {
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
	targetConflict := owner.targetDatabase != "" && (owner.targetDatabase == active.targetDatabase || owner.targetDatabase == active.controlDatabase)
	controlConflict := active.targetDatabase != "" && active.targetDatabase == owner.controlDatabase
	checkpointConflict := owner.controlDatabase == active.controlDatabase && owner.targetIdentity == active.targetIdentity
	if owner.targetEndpoint == active.targetEndpoint && (targetConflict || controlConflict || checkpointConflict) {
		return herror.NewError("CDC target database or checkpoint identity is already owned by another pipeline")
	}
	if owner.targetIdentity.SourceID == active.targetIdentity.SourceID {
		return herror.NewError("CDC source ID is already active")
	}
	return nil
}

// acquirePipelineOwnership 注册当前流水线的所有权，并返回退出时使用的释放函数。
func acquirePipelineOwnership(config pipelineConfig, tableOwner *dorisWriterIdentity) (func(), error) {
	owner, err := pipelineOwner(config)
	if err != nil {
		return nil, err
	}
	targetDatabase := owner.targetDatabase
	if tableOwner != nil {
		if len(config.Source.Tables) != 1 || tableOwner.Database != targetDatabase || tableOwner.Token == "" ||
			config.Identity.Generation != tableOwner.Stream.Generation {
			return nil, herror.NewError("CDC 单表同步必须使用同代次库流水线的 Doris 写入凭证")
		}
		// 目标库由父流水线持续独占，单表仅登记自己的检查点和复制客户端。
		owner.targetDatabase = ""
	}
	openPipelines.Lock()
	defer openPipelines.Unlock()
	parentActive := tableOwner == nil
	for active := range openPipelines.owners {
		if tableOwner != nil && active.targetIdentity == tableOwner.Stream && active.targetDatabase == targetDatabase &&
			active.targetEndpoint == owner.targetEndpoint && active.controlDatabase == owner.controlDatabase && active.sourceAddress == owner.sourceAddress {
			parentActive = true
		}
		if err := owner.conflict(active); err != nil {
			return nil, err
		}
	}
	if !parentActive {
		return nil, herror.NewError("CDC 单表同步的库流水线未持有目标库写入权")
	}
	openPipelines.owners[owner] = struct{}{}
	return func() {
		openPipelines.Lock()
		defer openPipelines.Unlock()
		delete(openPipelines.owners, owner)
	}, nil
}
