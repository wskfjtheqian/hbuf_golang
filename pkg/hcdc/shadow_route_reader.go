package hcdc

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
)

// ShadowRouteReader 通过具备读取权限的 SQL 存储查询已发布的路由。
// 消费方在一次完整的读取操作中使用同一份查询结果。
type ShadowRouteReader struct {
	store       *SQLShadowRouteStore
	routes      map[string]shadowRouteState
	fallbackTTL time.Duration
	mu          sync.Mutex
	confirmed   map[string]confirmedShadowReadRoute
}

type confirmedShadowReadRoute struct {
	route         ReadRoute
	checkedAt     time.Time
	invalidatedAt time.Time
}

// NewShadowRouteReader 创建每次查询权威元库的严格读路由客户端。
// 发布方和读取方应使用相同的路由配置。
func NewShadowRouteReader(store *SQLShadowRouteStore, configs []ShadowRoute) (*ShadowRouteReader, error) {
	return NewShadowRouteReaderWithCache(store, configs, 0)
}

// NewShadowRouteReaderWithCache 允许在权威元库暂时不可读时使用本进程最近确认的路由。
// fallbackTTL 为零时使用严格查询。缓存只保留经权威元库核验、修订号单调推进的路由。
func NewShadowRouteReaderWithCache(store *SQLShadowRouteStore, configs []ShadowRoute, fallbackTTL time.Duration) (*ShadowRouteReader, error) {
	if store == nil {
		return nil, herror.NewError("CDC read route consumer requires a SQL route store")
	}
	if fallbackTTL < 0 {
		return nil, herror.NewError("CDC read route fallback TTL cannot be negative")
	}
	router, err := NewShadowRouter(configs)
	if err != nil {
		return nil, err
	}
	return &ShadowRouteReader{
		store: store, routes: router.routes, fallbackTTL: fallbackTTL,
		confirmed: make(map[string]confirmedShadowReadRoute),
	}, nil
}

// Route 查询指定源当前已提交的读取目标。
// 已发布的路由缺失或无效时返回错误。
func (r *ShadowRouteReader) Route(ctx context.Context, sourceID string) (ReadRoute, error) {
	route, _, err := r.RouteWithStatus(ctx, sourceID)
	return route, err
}

// RouteWithStatus 查询已发布路由，并标记此结果是否来自故障期间的受限缓存。
func (r *ShadowRouteReader) RouteWithStatus(ctx context.Context, sourceID string) (route ReadRoute, cached bool, err error) {
	if err := ctx.Err(); err != nil {
		return ReadRoute{}, false, herror.Wrap(err)
	}
	state, exists := r.routes[sourceID]
	if !exists {
		return ReadRoute{}, false, herror.NewError("no shadow read route for source " + sourceID)
	}
	if r.fallbackTTL == 0 {
		route, err := r.store.load(ctx, state.config)
		return route, false, err
	}
	started := time.Now()
	route, err = r.store.load(ctx, state.config)
	r.mu.Lock()
	defer r.mu.Unlock()
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ReadRoute{}, false, herror.Wrap(ctxErr)
	}
	last := r.confirmed[sourceID]
	if err != nil {
		if isShadowReadTemporarilyUnavailable(err) {
			if !last.checkedAt.IsZero() && time.Since(last.checkedAt) < r.fallbackTTL {
				return last.route, true, nil
			}
		} else {
			r.invalidateCachedRoute(sourceID, last)
		}
		return ReadRoute{}, false, err
	}
	if route.Revision < last.route.Revision || (route.Revision == last.route.Revision &&
		(route.Database != last.route.Database || route.UsingShadow != last.route.UsingShadow ||
			route.Watermark != last.route.Watermark || !route.SwitchedAt.Equal(last.route.SwitchedAt))) {
		r.invalidateCachedRoute(sourceID, last)
		return ReadRoute{}, false, herror.WrapMsg(ErrShadowRouteRevisionChanged, "CDC consumer received an older or conflicting read route")
	}
	if route.Revision > last.route.Revision {
		last.checkedAt = time.Time{}
	}
	last.route = route
	if started.After(last.checkedAt) && !started.Before(last.invalidatedAt) {
		last.checkedAt = started
	}
	r.confirmed[sourceID] = last
	return route, false, nil
}

// invalidateCachedRoute 保留最近核验修订的下界，并暂停该源使用缓存。
func (r *ShadowRouteReader) invalidateCachedRoute(sourceID string, last confirmedShadowReadRoute) {
	last.checkedAt = time.Time{}
	last.invalidatedAt = time.Now()
	r.confirmed[sourceID] = last
}

// isShadowReadTemporarilyUnavailable 识别可降级的连接异常和短暂 SQL 故障。
func isShadowReadTemporarilyUnavailable(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var operation *net.OpError
	var network net.Error
	var server *mysql.MySQLError
	if errors.As(err, &server) {
		switch server.Number {
		case 1040, 1053: // 连接数耗尽、服务关闭。
			return true
		case 1205, 1213: // 锁等待超时、死锁。
			return true
		}
	}
	return errors.As(err, &operation) || (errors.As(err, &network) && network.Timeout()) ||
		errors.Is(err, driver.ErrBadConn) || errors.Is(err, mysql.ErrInvalidConn) ||
		errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}
