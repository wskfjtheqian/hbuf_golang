# hbuf_golang

Go 语言的基础库集合,核心是 HBuf 二进制序列化协议与 HRPC 远程调用框架,以及配套的缓存、配置、日志、分布式组件。

## 核心模块

### [hbuf](pkg/hbuf) — HBuf 二进制序列化协议

- 基于 descriptor 的二进制序列化,支持 Data/List/Map/Bytes/Int/Uint/Float/Bool 等类型
- `Marshal` / `Unmarshal` 以接口 `Data.Descriptors()` 描述结构布局,编解码零反射
- 与 [hrpc](pkg/hrpc) 深度集成,作为 JSON 之外的高性能编解码选项

### [hrpc](pkg/hrpc) — RPC 框架

- 多传输协议:HTTP / HTTPS / HTTP/2 / WS / WSS
- 编解码可插拔:JSON / HBuf
- 三层中间件抽象:请求层 / 响应层 / 业务层
- 统一响应模型 `Result[T]`、错误码集中映射、traceId 链路透传
- WebSocket 双工通信、连接管理与服务端推送
- 完整 Shutdown 机制,安全默认值(TLS 严格校验、HTTP/2 失败降级)

详见 [pkg/hrpc/readme.md](pkg/hrpc/readme.md)。

## 其他模块

| 模块 | 说明 |
|---|---|
| [hlog](pkg/hlog) | 日志,支持 ctx 链路 traceId、文件与 HTTP 输出 |
| [hcache](pkg/hcache) | 缓存:LRU、TinyLFU、内存缓存 |
| [hredis](pkg/hredis) | Redis 客户端与缓存封装 |
| [hcfg](pkg/hcfg) | 配置:文件 / etcd 热加载 |
| [hetcd](pkg/hetcd) / [hlock](pkg/hlock) | etcd 客户端 / 分布式锁(local、etcd、redis) |
| [hsql](pkg/hsql) | SQL 构建器与 DB 封装 |
| [hmq](pkg/hmq) | 消息队列(NATS) |
| [hcdc](pkg/hcdc) | CDC:canal → doris / 文件 |
| [hdedup](pkg/hdedup) | 去重引擎(shard + WAL) |
| [hsketch](pkg/hsketch) | 概率数据结构:布隆过滤器、Count-Min Sketch |
| [htime](pkg/htime) | 毫秒级时间(原子读写,无锁) |
| [hid](pkg/hid) / [hutl](pkg/hutl) | 雪花 ID / 通用工具(加密、邮件、队列、权重等) |
| [hhttp](pkg/hhttp) | HTTP 工具 |
| [hservice](pkg/hservice) | 服务编排 |
| [happ](pkg/happ) | 应用装配与健康检查 |
| [htest](pkg/htest) | 压测引擎 |

### 布隆过滤器选型参考

hsketch 内置了布隆过滤器实现;若需要生产级特性(删除、误判率估算),推荐:

- `github.com/bits-and-blooms/bloom`:最著名的 Go 布隆过滤器库,提供 `EstimatedFalsePositive` 计算;标准版不支持删除
- `github.com/seiflotfy/cuckoofilter`:布谷鸟过滤器,原生支持删除,误判率更低,很多场景下是布隆过滤器的现代替代品

## 构建与测试

项目使用 Go 1.27+ 工具链(`encoding/json/v2` 依赖),构建与测试需指定工具链版本:

```bash
GOTOOLCHAIN=go1.27.0 go build ./...
GOTOOLCHAIN=go1.27.0 go test ./...
GOTOOLCHAIN=go1.27.0 go test -race ./pkg/hrpc/
```
