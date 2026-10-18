# HService

HService 是基于 etcd 的微服务注册发现与 RPC 框架（见 [service.go](service.go) / [config.go](config.go)），构建在 hrpc 之上：向下复用 hrpc 的 HTTP/HTTPS 服务端与传输能力，向上提供服务注册、租约保活、服务发现、客户端路由与负载调度。默认使用 HBuf 二进制编解码。

## 特性

- **etcd 服务注册**：启动时将服务信息以 JSON 写入 etcd，key 形如 `hbuf-rpc://{addr}/{name}`；退出时按前缀注销
- **租约保活**：基于 etcd `concurrency.Session`（默认 TTL 5 秒），session 失效后由 keepSession 守护协程自动重新注册
- **服务发现**：watch etcd `hbuf-rpc://` 前缀，远端服务上下线自动增删客户端，无需重启
- **本地优先调用**：同进程已注册的服务与配置中标记 `local` 的地址走本地直调，不经过网络
- **可插拔调度策略**：随机 / 轮询 / 哈希 / 本地优先四种内置策略，支持自定义 `Dispatch`
- **HTTPS 零配置**：未配置证书时自动生成 ECDSA 自签名证书，默认即 HTTPS
- **配置热更新**：`SetConfig` 幂等，配置无变化直接返回；传 nil 则注销服务
- **泛型一键注册**：`Register[T]` 同时完成服务端注册与客户端构造，业务代码经 `GetClient(ctx, name)` 直接取客户端

## 文件结构

| 文件 | 说明 |
|---|---|
| [service.go](service.go) | 核心：`Service`、注册/注销/发现、客户端路由与调度、`Context` 封装 |
| [config.go](config.go) | 配置：`Config` / `Server` / `Client` / `Http` 结构体及 `Validate` / `Equal` |
| [server_test.go](server_test.go) | 服务注册、注销、发现的测试（依赖真实 etcd） |

## 快速开始

```go
// 1. 创建 etcd 客户端与服务
e := hetcd.NewEtcd()
if err := e.SetConfig(ctx, &hetcd.Config{Endpoints: []string{"127.0.0.1:2379"}}); err != nil {
    panic(err)
}
service := hservice.NewService(e)

// 2. 泛型注册服务（同时准备好服务端注册信息与客户端构造函数）
// RegisterUserService / NewUserServiceClient 由 hbuf 代码生成（见 hrpc readme）
hservice.Register(service, &UserService{}, RegisterUserService, NewUserServiceClient)

// 3. 设置配置并启动（注册到 etcd、启动 HTTPS RPC 服务、发现远端服务）
err := service.SetConfig(ctx, &hservice.Config{
    Server: &hservice.Server{
        Register:  true,
        List:      []string{"user"},
        LeaseTime: 5,
        Http:      &hservice.Http{Path: hutl.ToPointer("/rpc/")}, // 证书留空则自动生成自签名证书
    },
    Client: &hservice.Client{
        Find: true, // 开启服务发现
        Server: map[string][]string{
            "order": {"https://192.168.1.10:8080/rpc/"}, // 远端服务
        },
    },
})
if err != nil {
    panic(err)
}
defer service.Shutdown(ctx)
```

业务代码内通过 Context 获取客户端（无需传递 Service 指针）：

```go
func (s *UserService) GetOrder(ctx context.Context, req *GetOrderRequest) (*GetOrderResponse, error) {
    // 从上下文中的 Service 取客户端，默认策略为「本地优先 + 随机」
    client := hservice.GetClient(ctx, "order").(*OrderClient)
    return client.Query(ctx, req)
}
```

## 服务注册与发现

### 注册

`Register` 遍历 `Server.List` 中已安装的服务，以 `hbuf-rpc://{addr}/{name}` 为 key、`RegisterInfo` 的 JSON 为 value 写入 etcd，并绑定租约（`LeaseTime`，0 时默认 5 秒）：

```json
{"name": "user", "addr": "192.168.1.20:8080", "path": "/rpc/"}
```

`GetServerAddr` 在监听地址为 `:0` 时自动解析出本机 IP 与真实端口。`keepSession` 守护协程监视 session，失效后每 2 秒重试注册，保证进程存活期间注册信息持续有效。

### 发现

`Client.Find` 开启后异步执行 `Discovery`：先全量拉取 `hbuf-rpc://` 前缀下的服务，再 watch 后续变化：

- `PUT` 事件 → `parseRegisterInfo` → `addHttpClient`（按 host 复用 `hrpc.Client`，新增路由）
- `DELETE` 事件 → `parseDeleteInfo` → `delHttpClient`（过滤该 host 的非本地路由）

`waitSubscribe` 机制保证 `SetConfig` 阻塞至所有已安装服务都至少拥有一个客户端后才执行 `rpcServer.Init`，避免业务初始化时客户端尚未就绪。

## 客户端路由与调度

每个已安装服务对应一个 `client`（路由列表 + 调度器），路由分两类：

- **本地路由**：同进程已注册的服务，或配置中值为 `local` 的地址（`addLocalClient`），直接调用本地 init，零网络开销
- **HTTP 路由**：远端服务的 `hrpc.Client` 封装（`addHttpClient`）

`GetClient(name, dispatch)` 通过调度器从路由列表选择客户端，内置策略：

| 函数 | 说明 |
|---|---|
| `NewDispatchRandom()` | 随机选择一个路由 |
| `NewDispatchRoundRobin()` | 轮询调度 |
| `NewDispatchHash(hash)` | 按 hash 取模，同类请求固定路由 |
| `NewDispatchPriorityLocal(dispatch)` | 包装其他策略：存在本地路由则优先本地，否则回退 |

默认策略为 `NewDispatchPriorityLocal(NewDispatchRandom())`，即本地优先 + 随机。调度器返回的 index 以 `atomic.Int32` 累积，保证轮询跨调用连续。

## 配置说明

配置结构对应 YAML（标签首字母大写）：

```yaml
Codec: json            # 编解码器: json(默认) | hbuf
Transport: h2          # 传输层: http(默认) | h2(hrpc.H2Framer 帧批处理)
Server:
  Register: true        # 是否注册到 etcd
  Local: true           # 是否开启本地服务
  List:                 # 启动的服务列表（对应 Register 的服务名）
    - user
  LeaseTime: 5          # 租约 TTL（秒），0 时默认 5
  Http:
    Hostname: localhost # 主机名（可选）
    Address: ":0"       # 监听地址，:0 表示随机端口
    Crt: ./server.crt   # TLS 证书（与 Key 同时配置才生效，留空自动生成自签名证书）
    Key: ./server.key   # TLS 密钥
    Path: /rpc/         # RPC 路径前缀
Client:
  Find: true            # 是否开启服务发现（watch etcd）
  Server:               # 静态客户端配置：服务名 -> 地址列表
    order:
      - https://192.168.1.10:8080/rpc/
      - local            # "local" 表示走本地直调
```

`Codec` 与 `Transport` 同时作用于服务端与客户端（同构集群）：

- `Codec: json`（默认）使用 JSON 编解码，`Codec: hbuf` 使用 HBuf 二进制编解码
- `Transport: http`（默认）使用标准库 `net/http`；`Transport: h2` 使用 hrpc.H2Framer（单连接帧批处理，高并发下显著减少 syscall），服务端由 TLS 层完成 ALPN h2 协商
- 服务端证书为自签名或自定义，客户端统一跳过证书校验

`Client` 配置可选：为 nil 时跳过静态客户端建立，服务可作为纯服务端或纯发现节点运行。各结构体均提供 `Validate(ctx)`（启动前校验，如证书文件是否可加载、编解码器/传输层取值是否合法）与 `Equal(other)`（`SetConfig` 幂等判断的依据）。

`SetConfig` 行为：

- `cfg` 与当前配置相同 → 直接返回（幂等）
- `cfg` 为 nil → 注销服务并清空配置
- 否则先清理旧配置资源（注销旧 key、关闭旧 session、停止旧 keepSession），再按序执行：建立静态客户端 → 启动 HTTPS RPC 服务 → 注册到 etcd → 启动 keepSession → 按需开启 Discovery → 等待订阅完成后 `rpcServer.Init`

## Context 与中间件

`Context` 包装标准 `context.Context`，以 `reflect.TypeOf(&Context{})` 作为 Value key 携带 `*Service` 与调度器：

```go
// 服务端：将 Service 注入上下文的 hrpc 中间件
service := hservice.NewService(e,
    hservice.WithServerOption(hrpc.WithServerMiddleware(service.NewMiddleware())),
)

// 手动构造上下文
ctx := hservice.WithContext(context.Background(), hservice.WithContextService(service))

// 从上下文取回
svc := hservice.FromContext(ctx)          // *Service，上下文无 Service 时为 nil
client := hservice.GetClient(ctx, "user") // 直接取客户端，使用上下文携带的调度器
```

`GetClient(ctx, name)` 要求上下文中存在 `*Context`（由 `NewMiddleware` 注入或手动 `WithContext` 构造），否则返回 nil。

## 与 hrpc 的关系

| 能力 | 归属 |
|---|---|
| 协议编解码（HBuf/JSON）、`Result[T]` 响应模型、中间件 | hrpc |
| HTTP/HTTPS 服务端、客户端、连接池、H2Framer 传输 | hrpc |
| 服务注册、注销、租约保活、发现 | hservice |
| 客户端路由、调度、本地优先、订阅等待 | hservice |
| 编解码器/传输层选择（`Config.Codec` / `Config.Transport`） | hservice |

hservice 通过 `NewService` 默认启用 JSON 编解码（`WithServerEncoder/WithServerDecode` + 客户端 `WithClientEncoder/WithClientDecode`）；`Config.Codec` 优先于默认值与 `WithServerOption` 自定义项，切换时在 `SetConfig` 中重建 `rpcServer` 并重新注册已安装服务。`WithServerOption` / `WithClientOption` 可将任意 hrpc 选项透传给底层服务端与发现的客户端。

## 测试说明

[server_test.go](server_test.go) 的测试分为两类：

### 离线单元测试（默认执行）

不依赖外部环境，覆盖：

- 配置校验与相等性：`TestConfig_Validate` / `TestConfig_Equal` / `TestConfig_Equal_FieldDiff` / `TestConfig_CodecTransport_Defaults` / `TestConfig_Validate_CodecTransport` / `TestConfig_Equal_CodecTransport` / `TestHttp_Validate_Cert` / `TestServer_Validate` / `TestClient_Validate`
- 四种调度策略及边界：`TestDispatchRoundRobin` / `TestDispatchRoundRobin_Single` / `TestDispatchRandom` / `TestDispatchHash` / `TestDispatchHash_LargeHash` / `TestDispatchPriorityLocal_Fallback`
- 上下文传递：`TestWithContext_FromContext` / `TestContext_Value` / `TestFromContext_NoService` / `TestGetClient_NoContext` / `TestGetClient_NotInstalled`
- 未启动/未配置态：`TestGetServerAddr_NotStarted` / `TestShutdown_NotStarted` / `TestRegister_NoConfig` / `TestDeregister_NoEtcdClient`
- `Client` 为 nil 时 `SetConfig` 不 panic：`TestSetConfig_NilClient`

### 集成测试（环境变量门控）

以下测试验证完整注册→注销流程、热更新、服务发现、注销安全性，需设置环境变量 `HSERVICE_TEST_ETCD` 指向可用 etcd 才会执行，否则自动跳过：

- `TestService_RegisterAndDeregister`：注册→断言 etcd key→注销→断言 key 删除
- `TestService_SetConfigTwice`：幂等路径与完整重跑路径均不 panic、不阻塞
- `TestService_Deregister_EmptyAddr`：RPC 服务未启动时注销返回错误，不误删其他服务的注册信息
- `TestService_Discovery`：服务发现自动建立路由，远端注销后路由自动移除
- `TestService_StaticHttpClient`：静态 URL 配置建立 HTTP 客户端路由（非本地 init）
- `TestService_Register_Disabled`：`Register=false` 时不写入注册信息
- `TestService_SetConfig_WaitTimeout`：客户端未就绪时 `SetConfig` 阻塞至 ctx 超时
- `TestService_Shutdown_Started`：启动后 `Shutdown` 正常关闭
- `TestService_EndToEnd_JsonHttp`：JSON 编解码 + 标准库 HTTP 传输端到端 RPC 调用（未配置 Codec，验证默认 JSON 路径）
- `TestService_EndToEnd_HBufH2`：HBuf 编解码 + H2Framer 传输端到端 RPC 调用

```shell
HSERVICE_TEST_ETCD=127.0.0.1:2389 go test -count=1 -v -race ./pkg/hservice/
```
