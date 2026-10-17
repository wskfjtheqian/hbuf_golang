# HRPC

HRPC 是一个支持多传输协议(HTTP / HTTPS / HTTP/2 / WS / WSS)和多编解码格式(JSON / HBuf)的 RPC 框架。业务代码不依赖具体传输协议与编码格式,通过三层中间件抽象实现传输无关。

## 特性

- **传输无关**:同一套服务注册与调用代码可跑在 HTTP、HTTPS、WS、WSS 上
- **编解码可插拔**:默认 JSON(`encoding/json/v2`),可切换 HBuf 二进制协议
- **三层中间件**:请求层 `RequestMiddleware`、响应层 `ResponseMiddleware`、业务层 `Middleware`
- **统一响应模型**:`Result[T]` 封装 code/msg/data,错误码与 HTTP 状态码集中映射
- **traceId 链路透传**:`hlog.WithContext` 写入的 traceId 经 header 自动透传到对端 handler
- **WebSocket 双工**:连接上可同时发起请求与服务端推送,连接管理(ClientManager)支持按 id 定向推送
- **安全默认值**:TLS 证书默认严格校验;HTTP/2 配置失败降级 HTTP/1.1 而非退出进程
- **完整 Shutdown**:`Server.OnShutdown` 钩子、`HttpServer.Shutdown`、`WebSocketServer.Shutdown` 关闭监听器与活跃连接

## 快速开始

### 定义服务(通过 hbuf 代码生成)

```go
// 生成代码示例(见 hbuf_server_test.go)
func RegisterHbufService(r hrpc.ServerRegister, server HbufService) {
	r.Register(0, "hbuf_service", server,
		&hrpc.Method{
			Name: "hbuf_method",
			WithContext: func(ctx context.Context) context.Context { return ctx },
			Decode: func(decoder func(v hbuf.Data) (hbuf.Data, error)) (hbuf.Data, error) {
				req := &HbufRequest{}
				_, err := decoder(req)
				return req, err
			},
			Handler: func(ctx context.Context, req any) (any, error) {
				return server.HbufMethod(ctx, req.(*HbufRequest))
			},
		},
	)
}
```

### HTTP 服务端

```go
rpcServer := hrpc.NewServer()
RegisterHbufService(rpcServer, &TestHbufService{})

server := hrpc.NewHttpServer("/rpc/", rpcServer)
mux := http.NewServeMux()
mux.Handle("/rpc/", server)
go http.ListenAndServe(":8080", mux)
```

### HTTP 客户端

```go
httpClient := hrpc.NewHttpClient("http://localhost:8080/rpc")
rpcClient := hrpc.NewClient(httpClient.Request)
testClient := NewHbufServiceClient(rpcClient) // 生成代码

resp, err := testClient.HbufMethod(context.Background(), &HbufRequest{Name: "test"})
```

### WebSocket 服务端与客户端

```go
// 服务端:绑定到 HTTP 路径或独立监听
server := hrpc.NewWebSocketServer(rpcServer.Response)
go server.ListenAndServe(context.Background(), ":8081")
// 或者通过 http.Handler 挂载:
// http.HandleFunc("/socket", func(w http.ResponseWriter, r *http.Request) {
//     server.ServeHTTP(w, r, "client-id", onConnect, onClose)
// })

// 客户端
client := hrpc.NewWebSocketClient("ws://localhost:8081/socket", nil)
err := client.Connect(context.Background())
rpcClient := hrpc.NewClient(client.Request)
```

### HBuf 编解码

```go
rpcServer := hrpc.NewServer(
	hrpc.WithServerEncoder(hrpc.NewHBufEncode()),
	hrpc.WithServerDecode(hrpc.NewHBufDecode()),
)
// 客户端同理:WithClientEncoder/WithClientDecode
// WebSocket:WithWebSocketClientEncode/WithWebSocketClientDecode(客户端)、WithWebSocketServerEncode/WithWebSocketServerDecode(服务端)
```

## 核心概念

### Handler 返回值约定

```go
func(ctx context.Context, req any) (any, error)
```

- 返回 `io.Reader`:流式响应,直接拷贝到客户端
- 返回 `hbuf.Data`:包装为 `Result[T]` 编码响应
- 返回 `error` 且实现 `ResultI`:按错误码编码响应体
- 返回 nil 数据:返回明确错误(不 panic)

### 统一响应模型

```go
type Result[T hbuf.Data] struct {
	Code int32  `json:"code"`
	Msg  string `json:"msg"`
	Data T      `json:"data"`
}
```

- `CodeNotFound = -1`:方法未找到,映射 HTTP 404
- `StatusFromError(err) (int, bool)`:错误 → HTTP 状态码集中映射,not found → 404,其余 → 500 + 日志
- 客户端解码:`hrpc.NewResultResponse[*HbufResponse]()` 作为 `Invoke` 的 response 参数

### traceId 链路透传

```go
// 调用方
ctx := hlog.WithContext(context.Background(), traceId)
rpcClient.Invoke(ctx, 0, "service", "method", req, resp)

// 服务端 handler 内
traceId := hlog.FromContext(ctx) // 自动从 X-Trace-Id header 恢复
```

### InvokeServerFunc 反射调用约定

`InvokeServerFunc(ctx, obj, method, args)` 通过反射调用方法,args 为 JSON 字符串:

- 0 个返回值:视为成功
- 1 个返回值:必须为 error
- 2 个返回值:第一个为数据,第二个必须为 error
- 不符合约定时返回明确错误(不 panic)

## 配置选项

### Server

| 选项 | 说明 |
|---|---|
| `WithServerMiddleware` | 业务层中间件 |
| `WithServerDecode` / `WithServerEncoder` | 编解码器 |
| `OnShutdown(hook)` | 注册关闭钩子,`Shutdown` 时按序执行 |

### HttpClient

| 选项 | 说明 |
|---|---|
| `WithRequestMiddleware` | 请求层中间件 |
| `WithTimeout` | 整体超时 |
| `WithConnTimeout` | 连接超时 |
| `WithMaxConns` / `WithMaxIdleConns` / `MaxIdleConnsPerHost` / `WithIdleConnTimeout` | 连接池配置 |
| `WithTLSConfig` | 自定义 TLS 配置 |
| `WithInsecureSkipVerify` | 跳过证书校验(仅测试环境,默认严格校验) |

### WebSocketClient

| 选项 | 说明 |
|---|---|
| `WithWebSocketClientRequestMiddleware` / `WithWebSocketClientResponseMiddleware` | 请求/响应中间件 |
| `WithWebSocketClientEncode` / `WithWebSocketClientDecode` | 编解码器 |
| `WithWebSocketClientPingInterval` / `WithWebSocketClientPongWait` / `WithWebSocketClientIsSendPing` | 心跳配置 |
| `WithWebSocketClientRequestTimeout` | 请求超时(默认 30s) |
| `WithWebSocketClientTLSConfig` / `WithWebSocketClientInsecureSkipVerify` | TLS 配置(默认严格校验) |

### WebSocketServer

| 选项 | 说明 |
|---|---|
| `WithWebSocketServerRequestMiddleware` / `WithWebSocketServerResponseMiddleware` | 请求/响应中间件 |
| `WithWebSocketServerEncode` / `WithWebSocketServerDecode` | 编解码器 |
| `WithWebSocketServerPingInterval` / `WithWebSocketServerPongWait` / `WithWebSocketServerIsSendPing` | 心跳配置 |
| `WithWebSocketServerProtocol` | 子协议校验 |

### 服务端推送与连接管理

```go
// 服务端向指定客户端推送请求(请求需在客户端注册 response 处理)
server.Request(ctx, path, notification, callback)

// 按 id 关闭客户端
server.CloseClient(ctx, "client-id")

// 关闭监听器与全部活跃连接
server.Shutdown(ctx)
```

## Shutdown

```go
// 注册自定义关闭钩子
rpcServer.OnShutdown(func(ctx context.Context) error {
	return db.Close()
})

// HTTP:等待在途请求完成并执行钩子
httpServer.Shutdown(ctx)

// WebSocket:关闭监听器与全部活跃连接
wsServer.Shutdown(ctx)
```

## 性能对比

测试环境:goos: darwin, goarch: amd64, Go 1.27.0, CPU: Intel(R) Core(TM) i9-9980HK @ 2.40GHz, 16 线程(2026-10-17 实测,含 Result descriptor 单例缓存优化)。

### 串行基准(单连接循环,`Benchmark_HRPC_HTTP`)

```text
Benchmark_HRPC_HTTP/HRPC_HTTP_HBufEncode-16   	   38892	     88256 ns/op	    8564 B/op	     111 allocs/op
Benchmark_HRPC_HTTP/HRPC_HTTP-16              	   39855	     99060 ns/op	    8662 B/op	     111 allocs/op
Benchmark_HRPC_HTTP/HRPC_HTTPS-16             	   26208	    136692 ns/op	   11156 B/op	     114 allocs/op
Benchmark_HRPC_HTTP/HRPC_WS-16                	   38295	     83183 ns/op	    6071 B/op	      71 allocs/op
Benchmark_HRPC_HTTP/HRPC_WSS-16               	   35740	     95557 ns/op	    5975 B/op	      67 allocs/op
Benchmark_HRPC_HTTP/Http-16                   	   39900	     89972 ns/op	    7647 B/op	      93 allocs/op
Benchmark_HRPC_HTTP/Https-16                  	   26875	    134806 ns/op	   10755 B/op	      98 allocs/op
Benchmark_HRPC_HTTP/GRPC-16                   	   34644	    103470 ns/op	    8799 B/op	     153 allocs/op
```

串行场景结论:HRPC_HTTP(99.1 µs)与 HRPC_WS(83.2 µs)均快于 GRPC(103.5 µs);HRPC_HTTPS 因串行单连接下每次请求的 TLS 加解密开销,比明文场景慢约 38%(对照裸 Http/Https 同样相差 50%)。

### 并发基准(1000 并发窗口,相同 TLS 条件)

```text
HRPC_HTTPS_HBuf   14467 ns/op   10972 B/op   115 allocs/op
GRPC_Plain         9896 ns/op    8634 B/op   137 allocs/op
GRPC_TLS           9777 ns/op    8647 B/op   137 allocs/op
```

并发场景 HRPC 约为 gRPC 的 1.5 倍耗时,差距主要来自每请求分配(多 2.3 KB)与 h2 帧发送策略;编解码本身已与 protobuf 相当(纯编解码 HBuf 231 ns vs protobuf 289 ns,小对象)。

> 注意:HRPC 走明文 HTTP/1.1 时无多路复用,高并发下连接爆炸会耗尽本地端口,并发压测务必使用 HTTPS(HTTP/2)或 WebSocket。

> 测试命令:`GOTOOLCHAIN=go1.27.0 go test -bench Benchmark_HRPC_HTTP -run '^$' ./pkg/hrpc/ -benchmem -benchtime=3s`
