# HRPC 包分析报告与问题修复清单

> 本文件由代码审查生成，供后续对话了解 hrpc 包架构、定位并修复已知问题。
> 分析基准：v2.0.0 分支，2026-10-17。

## 一、包概览

HRPC 是 HBuf 生态的 RPC 框架：**传输无关 + 编解码可插拔**，支持 HTTP/HTTPS/HTTP2 与 WebSocket（含双工服务端推送），默认 JSON 编解码（兼容前端），可切换 HBuf 二进制协议获得高性能（实测 HRPC-HTTP 66µs vs gRPC 114µs，见 [readme.md](readme.md)）。

依赖：`herror`、`hlog`、`hservice`、`htime`、`gobwas/ws`、`golang.org/x/net/http2`、`encoding/json/v2`（需 Go 1.27+，测试须加 `GOTOOLCHAIN=go1.27.0` 前缀）。

## 二、文件职责清单

| 文件 | 职责 |
|---|---|
| [rpc.go](rpc.go) | 核心：`Type` 消息类型、`Request/Response/Handler` 三抽象 + 中间件、`Context`、`Method`、`Result[T]` 统一响应、`Encoder/Decoder`（JSON/HBuf）、`Server`/`Client` 调度 |
| [http.go](http.go) | HTTP 传输：`HttpClient`（HTTP/2、连接池）、`HttpServer`（http.Handler） |
| [websocket.go](websocket.go) | WS 传输：`WebSocketClient/Server`、双工收发、心跳、`ClientManager` 连接分片管理、按 key 定向推送 |
| [reflect.go](reflect.go) | 反射工具：`GetServerAndFuncName`、`InvokeServerFunc` |
| [hbuf_test.go](hbuf_test.go) | 测试用请求/响应结构体（HBuf descriptor 模板） |
| [hbuf_server_test.go](hbuf_server_test.go) | 服务注册模板：`RegisterXxxService` + `Method` 结构 + 默认 NotFound 实现 |
| [http_test.go](http_test.go) | HTTP 测试：JSON/HBuf 编解码、base64 中间件、HTTPS、与 gRPC 对比 benchmark |
| [websocket_test.go](websocket_test.go) | WS 测试：RPC、100 并发、加密中间件、traceId 透传、关闭竞态 |
| [proto_grpc_test.go](proto_grpc_test.go) | gRPC 对比测试（proto 生成代码） |

## 三、核心架构与关键流程

### 分层抽象

- `Handler(ctx, req any) (any, error)`：业务方法，三层中间件（`Middleware`）洋葱包裹
- `Request(ctx, path, notification, callback) (io.ReadCloser, error)`：客户端传输注入（HttpClient/WebSocketClient 均实现）
- `Response(ctx, path, writer, reader, header) error`：服务端传输回调
- `Encoder(writer) func(hbuf.Data) error` / `Decoder(reader) func(hbuf.Data) error`：编解码可插拔

### Server.Response 流程（rpc.go L420-470）

1. 按 path 查方法（`methods[name+"/"+key]`），未找到返回 `NewResult(-1, "method not found", nil)`（作为 error）
2. `method.Decode` 回调解码请求体（nil 时请求体原样作 io.Reader 传入）
3. 注入 `hrpc.Context`（header/tags/method）→ traceId 处理 → 中间件链调用 Handler
4. 响应为 `io.Reader` 时流式 `io.Copy`；否则包成 `NewResult(0, "ok", data)` 编码

### Client.Invoke 流程（rpc.go L557-597）

1. callback 编码请求体（`io.Reader` 直拷 / `hbuf.Data` 编码）
2. `c.request` 发传输请求，`response == nil` 视为 notification
3. 响应为 `ResultI` 时解码 + 校验 code，返回 `GetData()`

### WebSocket 双工（websocket.go）

- 帧格式：`WebSocketData{Type, Header, Data, Id, Path, Status}`（HBuf descriptor，L69-77），payload 二次编解码
- 请求-响应：`Id` + `responseMap[id] chan` 关联；`TypeNotification` 单向；`TypePing/Pong` 心跳
- 收发两个 goroutine：读 goroutine 分发，写 goroutine 串行写帧
- `ClientManager`：FNV 哈希 + 2 的幂分片（`NumCPU*8`），`Swap/Get/Del/CompareAndDelete/Range`
- 发送统一走 `send()`（select 写通道 / done channel），连接关闭后返回 "connection is closed"

### Result[T]（rpc.go L160-211）

`{Code int32, Msg string, Data T}`，`Data` 是**接口字段**。注意 `NewResult`/`NewResultResponse` 用接口 data word 偏移 hack 构造 descriptor（见问题 3）。

## 四、问题清单

### 🔴 Bug（必须修复）

#### 1. traceId 透传逻辑反转 ✅ 已修复

- **位置**：[rpc.go](rpc.go) L452-458
- **原状**：header 有 `X-Trace-Id` 时**不**写入 ctx；header 没有时反而把空串写入 ctx
- **影响**：服务端 handler 内 `hlog.FromContext` 拿不到客户端传来的 traceId，链路日志断裂
- **修复**：条件反转（`len(traceId) == 0` → `len(traceId) > 0`）
  ```go
  traceId := hlog.FromContext(ctx)
  if len(traceId) == 0 {
      traceId = GetHeader(ctx, "X-Trace-Id")
      if len(traceId) > 0 {
          ctx = hlog.WithContext(ctx, traceId)
      }
  }
  ```
- **测试**：`TestHttpService_TraceId`（Server.Response 单元级）、`TestWebsocket_TraceId`（WS 端到端）

#### 2. WebSocket 写通道关闭竞态（send on closed channel）✅ 已修复

- **位置**：[websocket.go](websocket.go) 读 goroutine 退出段（原 L188-191 `write.Store(nil)` 后 `close(*write)`）vs 发送点（原 L170 / L273 / L295 / L324 / L363，先 Load 判 nil 再 `*write <- data`）
- **影响**：连接断开瞬间，Load 成功 → close → send 的时间窗内会 panic: send on closed channel，进程崩溃
- **修复**：封装安全发送 `send()`，配合 `done` channel，**不再 close 写通道**：
  - `webSocket` 新增 `done chan struct{}`，`write` 字段由 `atomic.Pointer[chan *WebSocketData]` 简化为普通 `chan *WebSocketData`
  - 新增 `send()`：`select { case s.write <- data / case <-s.done }`，连接关闭返回 error
  - 读 goroutine 退出时 `close(s.done)`，写 goroutine 通过 `select <-s.done` 退出
  - 5 处发送点（Ping→Pong / Request notification / Request / onResponse 404 / onResponse 响应）全部改走 `send()`
  - 附带修复：`handleConnection` 中 `socket.onClose` 赋值移到 `run()` 之前，消除读 goroutine 读取 `onClose` 的数据竞争（`-race` 报告）
- **测试**：`TestWebsocket_SendAfterClose`（关闭后发送返回错误）、`TestWebsocket_CloseRace`（20 轮 × 10 goroutine 并发发送与关闭竞态，`-race` 通过）

#### 3. Result[T] descriptor 接口偏移 hack（待排查）

- **位置**：[rpc.go](rpc.go) L183-211
- **现状**：`NewResult`/`NewResultResponse` 用 `unsafe.Offsetof(ret.Data)+unsafe.Sizeof(&ret.Data)` 计算接口数据字偏移构造 descriptor，代码中保留「这里有递归死循环」注释
- **影响**：接口字段的 descriptor 偏移依赖运行时布局假设，切换编解码（HBuf）时可能编码/解码错位；`NewResult` 在 `data != nil` 分支存在递归构造隐患
- **状态**：本次未修复，需后续结合 HBuf 编解码场景专项验证

### 附带修复

- **[htime/time.go](file:///Users/dev/2.hbuf/hbuf_golang/pkg/htime/time.go)**：`NowTime()` 原实现用 `atomic.Pointer[time.Time]` 存指针、后台 goroutine 原地改写同一变量，`-race` 检测到读写竞争。改为 `atomic.Int64` 存 UnixMilli 值，语义不变（毫秒精度），彻底消除竞争。

## 五、验证

```bash
GOTOOLCHAIN=go1.27.0 go test -race -count=1 -run 'TestHttpService_TraceId|TestWebsocket_TraceId|TestWebsocket_SendAfterClose|TestWebsocket_CloseRace' ./pkg/hrpc/ -v
GOTOOLCHAIN=go1.27.0 go test ./pkg/htime/
```

### 既有问题排查结论（本次修复后与基线 v2.0.0 对比一致，非本次引入）

- `TestWebsocket_RPC`：`HbufServiceClient.HbufStream` 传 `response=nil` 被判定为 notification，WS `Request` 返回 `nil, nil`，客户端 nil 断言 panic（基线同样失败）。
- `TestWebsocket_Encrypt`：加密中间件将 base64 字节流写入 `rawMessage.Data`，JSON 帧编码时非法 JSON（基线同样失败）。
- 全量 `go test ./pkg/hrpc/`：多个测试重复 `http.Handle("/rpc/")` 注册同一 pattern，Go 1.22+ ServeMux panic（既有测试隔离问题，与本次修改无关）。
