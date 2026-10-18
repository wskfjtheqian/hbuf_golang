# HLog

HLog 是一个高性能的 Go 日志库（见 [log.go](log.go) / [ctx.go](ctx.go) / [file.go](file.go) / [http.go](http.go)），支持控制台、文件、HTTP 上传三种输出目标，内置 traceId 链路追踪、多级别文件路由、自动轮转与 ANSI 颜色输出。日志头格式与标准库 `log` 的 flag 体系兼容。

## 特性

- **多输出目标**：同一份日志可同时输出到控制台与文件/HTTP，由 `-log_out` 或 `SetOutTarget` 配置
- **多级别文件路由**：按 DEBUG/INFO/WARN/ERROR/EXIT 级别惰性创建独立日志文件；默认复杂模式下每个文件包含本级别及更低级别的日志，`-log_simple` 开启后仅包含本级别
- **stdout 缓冲输出**：控制台输出经 256KB bufio 缓冲，随 `Flush` / 定时刷盘批量落盘，降低 syscall 频率
- **traceId 链路追踪**：`WithContext` 写入的 traceId 随日志自动打印，配合 hrpc 经 header 透传
- **自动轮转与刷盘**：单文件超过 `MaxSize`（默认 1800MB）自动滚动；每 30 秒由 flushDaemon 定时 Flush
- **高性能**：`sync.Pool` 字节缓冲复用、`atomic` 字段无锁读写、256KB bufio 缓冲写文件
- **ANSI 颜色分级**：EXIT 紫底、ERROR 红底、WARN 黄底、DEBUG 绿底
- **零代码配置**：通过 `-log_out` / `-log_level` / `-log_simple` / `-log_err` 四个命令行 flag 即可完成部署期配置

## 文件结构

| 文件 | 说明 |
|---|---|
| [log.go](log.go) | 核心：`Level`、`Logger`、日志头格式化、全局 `std` Logger、命令行 flag 注册 |
| [ctx.go](ctx.go) | traceId：18 位 Base62 生成器（`NewTraceId`）与 Context 封装 |
| [file.go](file.go) | `fileWriter`：文件输出、大小轮转、日志文件命名 |
| [http.go](http.go) | `httpWriter`：HTTP 上传输出（当前为占位空实现） |

## 日志级别

| 级别 | 数值 | 说明 |
|---|---|---|
| DEBUG | 00000 | 调试信息 |
| INFO | 10000 | 一般信息 |
| WARN | 20000 | 警告 |
| ERROR | 30000 | 错误（默认输出级别） |
| EXIT | 40000 | 致命错误，打印日志后 panic |

- 低于输出级别（`SetOutLevel` / `-log_level`）的日志会被直接丢弃，默认仅输出 ERROR 及以上
- `SetLevelName` 可注册自定义级别名称；DEBUG/INFO/WARN/ERROR/EXIT 五个标准名称受保护，传入的 name 会被忽略

## 快速开始

### 全局 Logger（推荐）

包级 `std` Logger 提供带 ctx 的日志函数，自动从 ctx 读取 traceId 并打印：

```go
ctx := hlog.NewContext() // 自动生成 18 位 Base62 traceId

hlog.Info(ctx, "user %d login", 1)
hlog.Warn(ctx, "slow query: %s", "select * from t")
hlog.Error(ctx, "connect db failed: %v", err)
// hlog.Exit(ctx, "fatal: %v", err) // 打印日志后 panic

defer hlog.Flush() // 程序退出前刷盘（另有 30s 定时刷盘兜底）
```

输出格式：

```text
2026/10/18 12:00:00 [ERROR]  [9ZxKpQ2mW3vT8sN4hL] connect db failed: dial tcp ...
```

头部依次为：时间、级别（带 ANSI 颜色）、调用文件与行号（需 `Lshortfile` / `Llongfile`）、traceId。

### 自定义 Logger

```go
l := hlog.NewLogger("svc", hlog.LstdFlags|hlog.Lshortfile)
l.SetOutLevel(hlog.INFO) // 输出 INFO 及以上

l.Info("service started")
l.Error("failed: %v", err)
l.Flush()
```

自定义 Logger 的 `Debug/Info/Warn/Error/Exit/Output` 不携带 traceId；`Exit` 打印后同样会 panic。日志头由 `SetPrefix` / `SetFlags` 控制，支持 `Ldate`、`Ltime`、`Lmicroseconds`、`Llongfile`、`Lshortfile`、`LUTC`、`Lmsgprefix`（与标准库 log 语义一致）。`Close` 可停止定时刷盘 goroutine 并刷盘（进程常驻的全局 `std` Logger 无需调用）。

### 自定义输出目标

实现 `SyncWriter` 接口并通过 `SetOutTarget` 注册，按级别惰性创建：

```go
type SyncWriter interface {
    Flush() error
    Sync() error
    Write(level Level, p []byte) (n int, err error)
    Compare(call func(v1 Level, v2 Level) bool)
}
```

## traceId 链路追踪

```go
traceId := hlog.NewTraceId() // 18 位 Base62：42 位毫秒时间戳 + 65 位随机熵

ctx := hlog.WithContext(context.Background(), traceId) // traceId 为空时自动生成
id := hlog.FromContext(ctx)                            // 从上下文取回
```

- `Context` 以 `reflect.TypeOf(&Context{})` 作为 Value 的 key，`FromContext` 按同类型取回
- 配合 hrpc 时，traceId 随请求 header 自动透传到对端 handler（见 hrpc readme 的 traceId 链路透传）

## 输出目标

### 控制台

默认开启，每条日志同时写入 `os.Stdout`；`-log_err=false` 或 `setOutError(false)` 关闭。

### 文件

配置 `-log_out=./logs` 后按级别惰性创建独立文件，命名格式：

```text
{程序名}.{主机名}.{用户名}.{级别}.{yyyyMMdd-HHmmss}.{pid}.log

server.mac1.qian.INFO.20261018-120000.12345.log
```

- 写入使用 256KB bufio 缓冲，由 flushDaemon 每 30 秒刷盘
- 单文件超过 `MaxSize`（默认 1800MB，可直接修改该变量）时自动创建新文件
- 级别路由（默认复杂模式）：每个日志文件包含「本级别及更低级别」的日志，即低级别日志也会写入更高级别的文件；`-log_simple=true` 时每个文件仅包含本级别日志
- 写入文件前自动剥离 ANSI 颜色码，文件内容为纯文本
- 同一秒内多次轮转时，自动以 `.1`、`.2` 序号后缀创建新文件，避免同名覆盖

### HTTP

配置 `-log_out=http://...` 时走 `httpWriter`，当前为占位空实现，上传逻辑待扩展。

## 命令行参数

| flag | 默认值 | 说明 |
|---|---|---|
| `-log_out` | 无 | 日志输出目标，支持目录路径（文件输出）与 http(s) 地址（HTTP 上传） |
| `-log_level` | 30000 | 输出级别：DEBUG(00000)/INFO(10000)/WARN(20000)/ERROR(30000)/EXIT(40000) |
| `-log_simple` | false | 简单模式：日志文件仅包含当前级别的日志；关闭时为复杂模式（默认），日志同时写入更低级别的文件 |
| `-log_err` | true | 是否同时输出日志到控制台 |

## 性能设计

- **缓冲复用**：日志头与消息在 `sync.Pool` 字节缓冲中拼装，单缓冲超过 64KB 时归还前清空，避免大对象长期驻留
- **无锁读取**：`prefix` / `flag` / `outLevel` 等热路径字段均以 `atomic` 存储，写日志仅锁 `Logger.lock` 保护 writer 表
- **批写输出**：stdout 与 fileWriter 均以 256KB bufio 缓冲聚合写盘，降低 syscall 频率
