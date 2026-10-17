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

测试环境:goos: darwin, goarch: amd64, Go 1.27.0, CPU: Intel(R) Core(TM) i9-9980HK @ 2.40GHz, 16 线程(2026-10-17 实测)。

### 串行基准(单连接循环,`-benchmem -benchtime=3s`)

多传输协议对照(`Benchmark_HRPC_HTTP`):

```text
Benchmark_HRPC_HTTP/HRPC_HTTP_HBufEncode-16   	   38918	     93520 ns/op	    8637 B/op	     111 allocs/op
Benchmark_HRPC_HTTP/HRPC_HTTP-16              	   39027	     92214 ns/op	    8688 B/op	     111 allocs/op
Benchmark_HRPC_HTTP/HRPC_HTTPS-16             	   26356	    137735 ns/op	   11153 B/op	     114 allocs/op
Benchmark_HRPC_HTTP/HRPC_WS-16                	   42522	     88886 ns/op	    6069 B/op	      71 allocs/op
Benchmark_HRPC_HTTP/HRPC_WSS-16               	   36957	     95920 ns/op	    5978 B/op	      67 allocs/op
Benchmark_HRPC_HTTP/Http-16                   	   39805	     89439 ns/op	    7643 B/op	      93 allocs/op
Benchmark_HRPC_HTTP/Https-16                  	   26570	    134692 ns/op	   10754 B/op	      98 allocs/op
Benchmark_HRPC_HTTP/GRPC-16                   	   34720	    104728 ns/op	    8823 B/op	     153 allocs/op
```

其中 `Http`/`Https` 为裸标准库 JSON 对照,`GRPC` 为 protobuf 对照。

h2 传输对照(`Benchmark_H2Framer_VS_GRPC`,均为 HTTP/2 单连接):

```text
Benchmark_H2Framer_VS_GRPC/HRPC_H2Framer-16                     	   26863	    132511 ns/op	   12913 B/op	     137 allocs/op
Benchmark_H2Framer_VS_GRPC/HTTP2_StdTransport-16                	   29548	    123777 ns/op	   12223 B/op	     124 allocs/op
Benchmark_H2Framer_VS_GRPC/GRPC-16                              	   33556	    108750 ns/op	    9840 B/op	     153 allocs/op
Benchmark_H2Framer_VS_GRPC/HRPC_H2Framer+FramerServer-16        	   30324	    109158 ns/op	   11615 B/op	     146 allocs/op
Benchmark_H2Framer_VS_GRPC/HTTP2_StdTransport+FramerServer-16   	   35259	    103410 ns/op	   11012 B/op	     135 allocs/op
```

- `HRPC_H2Framer`:自研 H2FramerTransport 客户端 + x/net http2.Server 服务端
- `HTTP2_StdTransport`:标准库 x/net http2.Transport 客户端 + x/net http2.Server 服务端
- `HRPC_H2Framer+FramerServer`:全自研 h2 链路(H2FramerTransport 客户端 + H2FramerServer 服务端)
- `HTTP2_StdTransport+FramerServer`:标准库 h2 客户端 + 自研 H2FramerServer 服务端

串行结论:全自研 h2 链路(109.2 µs)与 gRPC(108.8 µs)持平;HRPC_WS(88.9 µs)为最快路径;HRPC_HTTP(92.2 µs)快于 gRPC 约 12%;TLS 场景(HRPC_HTTPS 137.7 µs)慢于明文约 49%,为串行单连接每请求加解密开销(裸标准库 Https 同样慢 51%,属 TLS 固有成本)。

### 并发基准

请求延迟矩阵(1000 并发窗口 × 3 请求,`Test_Concurrency_H2Framer_VS_GRPC`;h2/gRPC 组为单连接,WSS 为 33 连接分摊):

```text
HRPC_H2Framer                      40.1 µs/req   24961 qps   ratio 3.52
HTTP2_StdTransport                 34.3 µs/req   29157 qps   ratio 3.01
HRPC_H2Framer+FramerServer         11.4 µs/req   87778 qps   ratio 1.00
HTTP2_StdTransport+FramerServer    30.0 µs/req   33348 qps   ratio 2.63
HRPC_WSS                           11.4 µs/req   87914 qps   ratio 1.00
GRPC                               11.4 µs/req   87781 qps   ratio 1.00
```

全自研 h2 链路与 gRPC 持平(1.00 倍);HRPC_WSS(33 连接分摊 1000 并发窗口)同样与 gRPC 持平;x/net http2.Server 服务端组因响应逐帧 flush 慢 2-4 倍。

持续压测 qps(1000 goroutine × 10s,`Test_Concurrency`):

```text
标准库 HTTP/1.1              2980 qps   (明文 HTTP/1.1 无多路复用,连接爆炸)
HRPC_HTTPS                 60885 qps
HRPC_WS(WSS 33 连接)       5.3万 qps   (波动大,压测中部分请求超时)
GRPC                      122228 qps
HRPC_H2Framer+FramerServer 126678 qps   (全自研 h2 链路,反超 gRPC 约 4%)
```

> 注意:HRPC 走明文 HTTP/1.1 时无多路复用,高并发下连接爆炸会耗尽本地端口,并发压测务必使用 HTTPS(HTTP/2)或 WebSocket。

> 测试命令:
> - 串行:`GOTOOLCHAIN=go1.27.0 go test -bench 'Benchmark_HRPC_HTTP$|Benchmark_H2Framer_VS_GRPC$' -run '^$' ./pkg/hrpc/ -benchmem -benchtime=3s`
> - 并发矩阵:`GOTOOLCHAIN=go1.27.0 go test -run 'Test_Concurrency_H2Framer_VS_GRPC' -v ./pkg/hrpc/`
> - 持续压测:`GOTOOLCHAIN=go1.27.0 go test -run 'Test_Concurrency$' -v -timeout=180s ./pkg/hrpc/`
