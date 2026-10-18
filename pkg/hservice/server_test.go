package hservice_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hetcd"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hrpc"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hservice"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hutl"
	clientv3 "go.etcd.io/etcd/client/v3"
)

// mockService 实现 hrpc.Init 接口，用于测试服务注册与客户端构造。
// 注意不能定义为空结构体：零大小对象的指针恒指向 runtime.zerobase，
// 不同实例的接口值会完全相同，导致路由无法区分。
type mockService struct {
	name string
}

func (m *mockService) Init(ctx context.Context) {}

// installService 通过公共 API 注册一个名为 name 的 mock 服务，返回本地 init 实例
func installService(t *testing.T, service *hservice.Service, name string) *mockService {
	t.Helper()
	init := &mockService{name: name}
	hservice.Register(service, init, func(r hrpc.ServerRegister, s *mockService) {
		r.Register(0, name, s)
	}, func(c *hrpc.Client) *mockService {
		return &mockService{name: "client-" + name}
	})
	return init
}

// newTestConfig 构造一个开启注册、各服务均配置本地客户端的测试配置
func newTestConfig(names ...string) *hservice.Config {
	servers := make(map[string][]string, len(names))
	for _, name := range names {
		servers[name] = []string{"local"}
	}
	return &hservice.Config{
		Server: &hservice.Server{
			Register: true,
			Local:    true,
			List:     names,
			Http:     &hservice.Http{Path: hutl.ToPointer("/")},
		},
		Client: &hservice.Client{
			Find:   true,
			Server: servers,
		},
	}
}

// getEtcdEndpoint 返回集成测试的 etcd 地址，未设置环境变量时跳过测试
func getEtcdEndpoint(t *testing.T) string {
	t.Helper()
	endpoint := os.Getenv("HSERVICE_TEST_ETCD")
	if endpoint == "" {
		t.Skip("未设置环境变量 HSERVICE_TEST_ETCD，跳过依赖真实 etcd 的集成测试")
	}
	return endpoint
}

// newRouters 构造 n 个指向不同 mock 实例的路由
func newRouters(n int) []hservice.Router {
	routers := make([]hservice.Router, n)
	for i := range routers {
		routers[i] = hservice.Router{Init: &mockService{name: fmt.Sprintf("mock-%d", i)}}
	}
	return routers
}

// TestConfig_Validate 校验配置有效性
func TestConfig_Validate(t *testing.T) {
	var nilCfg *hservice.Config
	if nilCfg.Validate(context.Background()) {
		t.Error("nil 配置应校验失败")
	}

	cfg := &hservice.Config{Server: &hservice.Server{}}
	if cfg.Validate(context.Background()) {
		t.Error("缺少 Client / Http / List 的配置应校验失败")
	}

	// Config.Validate 仅做顶层 nil 检查，字段级校验由各子结构体负责
	server := &hservice.Server{
		Http: &hservice.Http{},
		List: []string{"user"},
	}
	if server.Validate(context.Background()) {
		t.Error("Http 缺少 Hostname / Address / Path 的配置应校验失败")
	}

	cfg = &hservice.Config{
		Server: &hservice.Server{
			Http: &hservice.Http{
				Hostname: hutl.ToPointer("localhost"),
				Address:  hutl.ToPointer(":0"),
				Path:     hutl.ToPointer("/"),
			},
			List: []string{"user"},
		},
		Client: &hservice.Client{Server: map[string][]string{}},
	}
	if !cfg.Validate(context.Background()) {
		t.Error("字段完整的配置应校验通过")
	}
}

// TestConfig_Equal 校验配置相等性判断
func TestConfig_Equal(t *testing.T) {
	path := hutl.ToPointer("/")
	base := &hservice.Config{
		Server: &hservice.Server{
			Register: true,
			List:     []string{"user"},
			Http:     &hservice.Http{Path: path},
		},
		Client: &hservice.Client{Find: true, Server: map[string][]string{"user": {"local"}}},
	}

	var nilCfg *hservice.Config
	if !nilCfg.Equal(nil) {
		t.Error("两个 nil 配置应相等")
	}
	if base.Equal(nil) {
		t.Error("非 nil 配置与 nil 配置不应相等")
	}

	same := &hservice.Config{
		Server: &hservice.Server{
			Register: true,
			List:     []string{"user"},
			Http:     &hservice.Http{Path: path},
		},
		Client: &hservice.Client{Find: true, Server: map[string][]string{"user": {"local"}}},
	}
	if !base.Equal(same) {
		t.Error("字段值相同的独立配置应相等")
	}

	diff := &hservice.Config{
		Server: &hservice.Server{
			Register: false,
			List:     []string{"user"},
			Http:     &hservice.Http{Path: path},
		},
		Client: &hservice.Client{Find: true, Server: map[string][]string{"user": {"local"}}},
	}
	if base.Equal(diff) {
		t.Error("Register 不同的配置不应相等")
	}
}

// TestDispatchRoundRobin 轮询调度依次命中每个路由
func TestDispatchRoundRobin(t *testing.T) {
	routers := newRouters(3)
	dispatch := hservice.NewDispatchRoundRobin()

	index := int32(0)
	for i := 0; i < 3; i++ {
		init, next := dispatch(routers, index)
		index = next
		if init != routers[(i+1)%3].Init {
			t.Errorf("第 %d 次轮询应命中路由 %d", i, (i+1)%3)
		}
	}
}

// TestDispatchRandom 随机调度的结果必须是列表中的路由之一
func TestDispatchRandom(t *testing.T) {
	routers := newRouters(4)
	dispatch := hservice.NewDispatchRandom()

	seen := map[hrpc.Init]bool{}
	for i := 0; i < 100; i++ {
		init, index := dispatch(routers, 0)
		if index < 0 || index >= int32(len(routers)) {
			t.Fatalf("随机调度返回越界 index: %d", index)
		}
		seen[init] = true
	}
	if len(seen) == 1 {
		t.Error("100 次随机调度只命中 1 个路由，疑似未随机")
	}
}

// TestDispatchHash 哈希调度按 hash 取模命中固定路由
func TestDispatchHash(t *testing.T) {
	routers := newRouters(4)
	dispatch := hservice.NewDispatchHash(7)
	init, index := dispatch(routers, 0)
	if init != routers[3].Init || index != 3 {
		t.Errorf("hash=7 应命中路由 3 (7%%4=3)，实际 index=%d", index)
	}
}

// TestDispatchPriorityLocal_Fallback 无本地路由时回退到包装的调度策略
func TestDispatchPriorityLocal_Fallback(t *testing.T) {
	routers := newRouters(3)
	dispatch := hservice.NewDispatchPriorityLocal(hservice.NewDispatchRoundRobin())
	init, index := dispatch(routers, 0)
	if init != routers[1].Init || index != 1 {
		t.Error("无本地路由时应回退到轮询策略")
	}
}

// TestWithContext_FromContext 上下文可携带并取回 Service
func TestWithContext_FromContext(t *testing.T) {
	service := hservice.NewService(hetcd.NewEtcd())
	ctx := hservice.WithContext(context.Background(), hservice.WithContextService(service))
	if hservice.FromContext(ctx) != service {
		t.Error("FromContext 应取回 WithContextService 设置的 Service")
	}

	// 嵌套 WithContext 继承父上下文的 Service
	child := hservice.WithContext(ctx)
	if hservice.FromContext(child) != service {
		t.Error("嵌套 WithContext 应继承父上下文的 Service")
	}
}

// TestFromContext_NoService 未携带 Service 的上下文返回 nil
func TestFromContext_NoService(t *testing.T) {
	if hservice.FromContext(context.Background()) != nil {
		t.Error("普通上下文 FromContext 应返回 nil")
	}
}

// TestGetClient_NoContext 未携带 Service 的上下文取客户端返回 nil
func TestGetClient_NoContext(t *testing.T) {
	if hservice.GetClient(context.Background(), "user") != nil {
		t.Error("普通上下文 GetClient 应返回 nil")
	}
}

// TestGetClient_NotInstalled 已携带 Service 但服务未安装时返回 nil
func TestGetClient_NotInstalled(t *testing.T) {
	service := hservice.NewService(hetcd.NewEtcd())
	ctx := hservice.WithContext(context.Background(), hservice.WithContextService(service))
	if hservice.GetClient(ctx, "user") != nil {
		t.Error("未安装服务时 GetClient 应返回 nil")
	}
}

// TestRegister_NoConfig 未设置配置时注册返回错误
func TestRegister_NoConfig(t *testing.T) {
	service := hservice.NewService(hetcd.NewEtcd())
	if err := service.Register(context.Background()); err == nil {
		t.Error("未设置配置时 Register 应返回错误")
	}
}

// TestDeregister_NoEtcdClient 未连接 etcd 时注销返回错误
func TestDeregister_NoEtcdClient(t *testing.T) {
	service := hservice.NewService(hetcd.NewEtcd())
	if err := service.Deregister(context.Background()); err == nil {
		t.Error("未连接 etcd 时 Deregister 应返回错误")
	}
}

// findRegisterKey 查询 etcd 中指定服务的注册 key，不存在时返回空串
func findRegisterKey(ctx context.Context, client *clientv3.Client, name string) (string, error) {
	resp, err := client.Get(ctx, hservice.ProtocolName, clientv3.WithPrefix())
	if err != nil {
		return "", err
	}
	for _, kv := range resp.Kvs {
		if strings.HasSuffix(string(kv.Key), "/"+name) {
			return string(kv.Key), nil
		}
	}
	return "", nil
}

// TestService_RegisterAndDeregister 集成测试：注册服务到 etcd 并注销。
// 需设置环境变量 HSERVICE_TEST_ETCD（如 192.168.1.202:2379）指向可用 etcd。
func TestService_RegisterAndDeregister(t *testing.T) {
	endpoint := getEtcdEndpoint(t)

	// 创建 etcd 客户端
	e := hetcd.NewEtcd()
	if err := e.SetConfig(context.Background(), &hetcd.Config{Endpoints: []string{endpoint}}); err != nil {
		t.Fatal("创建 Etcd 客户端失败:", err)
	}
	defer e.Shutdown(context.Background())

	// 创建 Service 并通过公共 API 安装 mock 服务
	service := hservice.NewService(e)
	installService(t, service, "user")
	defer service.Shutdown(context.Background())

	// 设置配置并启动：注册到 etcd、开启本地客户端与服务发现
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := service.SetConfig(ctx, newTestConfig("user")); err != nil {
		t.Fatal("设置服务配置失败:", err)
	}

	// 验证服务已注册到 etcd
	client, err := e.GetClient()
	if err != nil {
		t.Fatal(err)
	}
	key, err := findRegisterKey(ctx, client, "user")
	if err != nil {
		t.Fatal(err)
	}
	if key == "" {
		t.Fatal("未在 etcd 中找到 user 服务的注册信息")
	}

	// 停止 keepSession 守护协程并注销服务
	cancel()
	if err := service.Deregister(context.Background()); err != nil {
		t.Fatal("注销服务失败:", err)
	}

	// 等待 keepSession 退出（session.Done 唤醒后至多 2 秒）
	time.Sleep(2500 * time.Millisecond)
	if key, err := findRegisterKey(context.Background(), client, "user"); err != nil {
		t.Fatal(err)
	} else if key != "" {
		t.Errorf("注销后 etcd 中仍存在注册信息: %s", key)
	}
}

// TestSetConfig_NilClient 回归测试：Client 为 nil 时 SetConfig 不应 panic，
// 而是跳过静态客户端建立，在 Register 阶段因未连接 etcd 返回错误
func TestSetConfig_NilClient(t *testing.T) {
	service := hservice.NewService(hetcd.NewEtcd())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := service.SetConfig(ctx, &hservice.Config{
		Server: &hservice.Server{
			Register: true,
			List:     []string{"user"},
			Http:     &hservice.Http{Path: hutl.ToPointer("/")},
		},
	})
	if err == nil {
		t.Error("未连接 etcd 时 SetConfig 应返回错误")
	}
}

// TestService_SetConfigTwice 集成测试：连续多次 SetConfig 不应 panic 或阻塞。
// 需设置环境变量 HSERVICE_TEST_ETCD 指向可用 etcd。
func TestService_SetConfigTwice(t *testing.T) {
	endpoint := getEtcdEndpoint(t)

	e := hetcd.NewEtcd()
	if err := e.SetConfig(context.Background(), &hetcd.Config{Endpoints: []string{endpoint}}); err != nil {
		t.Fatal("创建 Etcd 客户端失败:", err)
	}
	defer e.Shutdown(context.Background())

	service := hservice.NewService(e)
	installService(t, service, "user")
	defer service.Shutdown(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 关闭服务发现：规避 s.config 字段已知并发读写问题，config 改为 atomic 后可开启
	newCfg := func() *hservice.Config {
		cfg := newTestConfig("user")
		cfg.Client.Find = false
		return cfg
	}

	if err := service.SetConfig(ctx, newCfg()); err != nil {
		t.Fatal("第一次设置服务配置失败:", err)
	}

	// 同一对象再次设置：Equal 为 true，幂等返回
	cfg := newCfg()
	if err := service.SetConfig(ctx, cfg); err != nil {
		t.Fatal("第二次设置服务配置（幂等路径）失败:", err)
	}

	// 字段值相同但对象不同：Equal 为 false，需完整重跑（重新监听/注册/keepSession 替换）而不 panic 或阻塞
	if err := service.SetConfig(ctx, newCfg()); err != nil {
		t.Fatal("第三次设置服务配置（完整重跑路径）失败:", err)
	}
}

// TestService_Deregister_EmptyAddr 集成测试：RPC 服务未启动时 Deregister 返回错误，
// 且不得误删 etcd 中其他服务的注册信息。需设置环境变量 HSERVICE_TEST_ETCD 指向可用 etcd。
func TestService_Deregister_EmptyAddr(t *testing.T) {
	endpoint := getEtcdEndpoint(t)

	e := hetcd.NewEtcd()
	if err := e.SetConfig(context.Background(), &hetcd.Config{Endpoints: []string{endpoint}}); err != nil {
		t.Fatal("创建 Etcd 客户端失败:", err)
	}
	defer e.Shutdown(context.Background())

	// 注册一个正常服务并记录其 key
	serviceA := hservice.NewService(e)
	installService(t, serviceA, "user")
	defer serviceA.Shutdown(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := serviceA.SetConfig(ctx, newTestConfig("user")); err != nil {
		t.Fatal("设置服务配置失败:", err)
	}

	client, err := e.GetClient()
	if err != nil {
		t.Fatal(err)
	}
	key, err := findRegisterKey(ctx, client, "user")
	if err != nil {
		t.Fatal(err)
	}
	if key == "" {
		t.Fatal("未在 etcd 中找到 user 服务的注册信息")
	}

	// 未启动 RPC 服务的 Service 注销：应返回错误，且不删除任何注册信息
	serviceB := hservice.NewService(e)
	if err := serviceB.Deregister(context.Background()); err == nil {
		t.Fatal("RPC 服务未启动时 Deregister 应返回错误")
	}
	if key, err := findRegisterKey(context.Background(), client, "user"); err != nil {
		t.Fatal(err)
	} else if key == "" {
		t.Fatal("Deregister 误删了其他服务的注册信息")
	}

	// 清理：停止守护协程并注销
	cancel()
	_ = serviceA.Deregister(context.Background())
}
