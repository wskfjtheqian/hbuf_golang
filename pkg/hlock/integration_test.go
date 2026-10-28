package hlock_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hetcd"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hredis"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// testEtcdEndpoint 返回集成测试使用的 etcd 地址，可用环境变量 HLOCK_TEST_ETCD 覆盖
func testEtcdEndpoint() string {
	if endpoint := os.Getenv("HLOCK_TEST_ETCD"); endpoint != "" {
		return endpoint
	}
	return "127.0.0.1:2389"
}

// testRedisAddr 返回集成测试使用的 Redis 地址，可用环境变量 HLOCK_TEST_REDIS 覆盖
func testRedisAddr() string {
	if addr := os.Getenv("HLOCK_TEST_REDIS"); addr != "" {
		return addr
	}
	return "127.0.0.1:6389"
}

var (
	testEtcd      *hetcd.Etcd
	testEtcdCtx   context.Context
	testEtcdSkip  string
	testRedis     *hredis.Redis
	testRedisCtx  context.Context
	testRedisSkip string
)

// TestMain 探测并初始化集成测试环境，不可用时相关测试跳过
func TestMain(m *testing.M) {
	testEtcdCtx, testEtcdSkip = initEtcd()
	testRedisCtx, testRedisSkip = initRedis()

	code := m.Run()

	if testEtcd != nil {
		_ = testEtcd.Shutdown(context.Background())
	}
	if testRedis != nil {
		_ = testRedis.Shutdown(context.Background())
	}
	os.Exit(code)
}

// initEtcd 快速探测并初始化 etcd 连接
func initEtcd() (context.Context, string) {
	endpoint := testEtcdEndpoint()

	client, err := clientv3.New(clientv3.Config{
		Endpoints:   []string{endpoint},
		DialTimeout: time.Second * 2,
	})
	if err != nil {
		return nil, fmt.Sprintf("无法连接 etcd %s: %v", endpoint, err)
	}
	statusCtx, cancel := context.WithTimeout(context.Background(), time.Second*2)
	defer cancel()
	if _, err := client.Status(statusCtx, endpoint); err != nil {
		_ = client.Close()
		return nil, fmt.Sprintf("etcd %s 不可用: %v", endpoint, err)
	}
	_ = client.Close()

	e := hetcd.NewEtcd()
	setErr := func() (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("etcd 初始化失败: %v", r)
			}
		}()
		return e.SetConfig(context.Background(), &hetcd.Config{Endpoints: []string{endpoint}})
	}()
	if setErr != nil {
		return nil, fmt.Sprintf("初始化 etcd %s 失败: %v", endpoint, setErr)
	}

	testEtcd = e
	return hetcd.WithContext(context.Background(), e), ""
}

// initRedis 快速探测并初始化 Redis 连接
func initRedis() (context.Context, string) {
	addr := testRedisAddr()
	dialTimeout := 2 * time.Second
	maxRetries := 0

	r := hredis.NewRedis()
	cfg := &hredis.Config{
		Addr:        &addr,
		DialTimeout: &dialTimeout,
		MaxRetries:  &maxRetries,
	}
	if err := r.SetConfig(context.Background(), cfg); err != nil {
		return nil, fmt.Sprintf("无法连接 redis %s: %v", addr, err)
	}

	testRedis = r
	return hredis.WithContext(context.Background(), r), ""
}
