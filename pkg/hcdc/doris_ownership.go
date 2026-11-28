package cdc

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hcdc/internal/spool"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
)

const dorisWriterRegistry = "cdc_writer_registry"

// ErrDorisWriterOwned 表示目标库已经绑定到其他持久缓冲或流水线。
var ErrDorisWriterOwned = herror.NewError("Doris target database already belongs to a different CDC writer")

type dorisWriterIdentity struct {
	Database string
	Stream   StreamIdentity
	Path     string
	Token    string
}

// loadDorisWriterIdentity 在持有 bootstrap 排他锁时，为原卷生成或读取同代次写入凭证。
func loadDorisWriterIdentity(directory, database string, identity StreamIdentity) (_ dorisWriterIdentity, resultErr error) {
	directory, err := resolvePipelineDirectory(directory)
	if err != nil {
		return dorisWriterIdentity{}, err
	}
	store, err := spool.Open(filepath.Join(directory, "writer"))
	if err != nil {
		return dorisWriterIdentity{}, err
	}
	defer func() { resultErr = errors.Join(resultErr, store.Close()) }()
	owner := dorisWriterIdentity{Database: strings.ToLower(database), Stream: identity, Path: directory}
	stored, err := store.Metadata()
	if err != nil {
		return dorisWriterIdentity{}, err
	}
	if stored != nil {
		var saved dorisWriterIdentity
		if err := json.Unmarshal(stored, &saved); err != nil {
			return dorisWriterIdentity{}, herror.WrapMsg(err, "decode CDC writer identity")
		}
		token, err := hex.DecodeString(saved.Token)
		if err != nil || len(token) != 32 || saved.Database != owner.Database || saved.Stream != owner.Stream || saved.Path != owner.Path {
			return dorisWriterIdentity{}, herror.NewError("CDC writer identity does not match the original stream and buffer directory")
		}
		return saved, nil
	}
	var token [32]byte
	if _, err := rand.Read(token[:]); err != nil {
		return dorisWriterIdentity{}, herror.Wrap(err)
	}
	owner.Token = hex.EncodeToString(token[:])
	encoded, err := json.Marshal(owner)
	if err != nil {
		return dorisWriterIdentity{}, herror.Wrap(err)
	}
	if _, err := store.Bind(encoded); err != nil {
		return dorisWriterIdentity{}, err
	}
	return owner, nil
}

// claimWriter 利用 Doris 目录对象的唯一名称注册跨进程写入权。
// 凭证不会到期。崩溃后原卷可恢复，其他卷在旧写入者完成隔离前不得接管。
func (d *DorisTarget) claimWriter(ctx context.Context, owner dorisWriterIdentity) error {
	if strings.EqualFold(d.config.ControlDatabase, dorisWriterRegistry) || strings.EqualFold(owner.Database, dorisWriterRegistry) {
		return herror.NewError("CDC writer registry is reserved for ownership records")
	}
	registry := quoteDoris(dorisWriterRegistry)
	if _, err := d.db.ExecContext(ctx, "CREATE DATABASE IF NOT EXISTS "+registry); err != nil {
		return wrapDorisRequestError(err, "create Doris writer registry")
	}
	if err := d.claimWriterObject(ctx, dorisWriterObject("target", owner.Database), owner.Token); err != nil {
		return herror.Wrap(fmt.Errorf("target database %q: %w", owner.Database, err))
	}
	stream := strings.ToLower(d.config.ControlDatabase) + "/" + dorisStreamKey(owner.Stream)
	return d.claimWriterObject(ctx, dorisWriterObject("stream", stream), owner.Token)
}

// dorisWriterObject 生成该 Doris 集群内唯一的目标或检查点所有权名称。
func dorisWriterObject(kind, key string) string {
	return quoteDoris(dorisWriterRegistry) + "." + quoteDoris(kind+"_"+digest([]byte(key))[:48])
}

// claimWriterObject 原子创建声明，并以读回核对结果作为唯一放行依据。
func (d *DorisTarget) claimWriterObject(ctx context.Context, name, token string) error {
	// CREATE VIEW 的目录注册在 Doris FE 仲裁，视图中只有生成的十六进制凭证。
	_, createErr := d.db.ExecContext(ctx, "CREATE VIEW IF NOT EXISTS "+name+" AS SELECT '"+token+"' AS owner_token")
	var actual string
	if err := d.db.QueryRowContext(ctx, "SELECT owner_token FROM "+name).Scan(&actual); err != nil {
		return wrapDorisRequestError(errors.Join(createErr, err), "verify Doris writer ownership")
	}
	if actual != token {
		return ErrDorisWriterOwned
	}
	return nil
}
