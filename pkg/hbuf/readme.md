# HBuf 二进制序列化协议

HBuf 是一个自定义的高性能二进制序列化协议（Go 实现，见 [data.go](data.go) / [encoder.go](encoder.go) / [decoder.go](decoder.go) / [descriptor.go](descriptor.go)）。

核心设计：

- **编译期生成 Descriptor**：由 `.hbuf` IDL 经代码生成器产出 `Descriptors()` 方法，基于 `unsafe.Offsetof` 字段偏移直接读写内存，运行时零反射。
- **变长整数编码**：整数按实际大小用 1~8 字节编码，小数值只占 1 字节。
- **空值省略**：结构体中为空（零值 / nil）的字段不写入。
- **类型演进兼容**：解码时支持跨类型转换；未知字段按类型跳过（`SkipValue`），新旧版本可互读。
- 类型系统覆盖 `int8~int64 / uint8~uint64 / float / double / bool / string / bytes / date / decimal`，以及 `Data`（结构体）、`List`、`Map`。

## 类型系统

| 值 | 常量 | 说明 |
|---|------|------|
| 0 | `TInt` | 有符号变长整数 |
| 1 | `TUint` | 无符号变长整数 |
| 2 | `TFloat` | 浮点数（IEEE-754 位模式） |
| 3 | `TBool` | 布尔（值编码在 TAG 内，见下） |
| 4 | `TBytes` | 字节序列（string / bytes / date / decimal 均以此承载） |
| 5 | `TList` | 列表 |
| 6 | `TMap` | 映射 |
| 7 | `TData` | 结构体 |

## 二进制格式

### TAG（1 字节）

每个值都以一个 TAG 字节开头，布局如下：

```
bit 7 6 5     4 3 2         1 0
┌─────────┬───────────┬─────────┐
│  类型   │ 值长度 - 1 │ ID 长度 │
└─────────┴───────────┴─────────┘
```

- **类型（3 bit）**：见上表。
- **值长度（3 bit）**：存储 `实际长度 - 1`，即实际长度为 1~8 字节。
- **ID 长度（2 bit）**：ID 占用的字节数，`0` = 无 ID，`1` = 1 字节，`2` = 2 字节。

> 实现：`WriterType`（encoder.go）/ `DecodeType`（decoder.go）。

### ID 编码

ID 为 `uint16`，小端序，占用 1~2 字节（`WriterId` / `DecodeId`）：`id <= 0xFF` 时 1 字节，否则 2 字节。`idLen = 0` 表示该值不带 ID（List 元素、Map 的 Key/Value）。

### 变长整数（VALUE）

- **无符号**：`LengthUint` / `EncodeUint64` / `DecodeUint64`，按实际大小 1~8 字节，小端序。
- **有符号**：`LengthInt` / `WriterInt64` / `DecodeInt64`，1~8 字节小端序，解码时以最高字节做符号扩展。
- **浮点**：`float32` / `float64` 按 IEEE-754 位模式分别转为 `uint32` / `uint64` 后按无符号变长编码（高位 0 字节自动省略）。

### 各类型字段格式（结构体字段，带 ID）

```
TInt / TUint / TFloat:   TAG | ID | VALUE
TBool:                   TAG | ID                     （无 VALUE，值编码在 TAG 中）
TBytes:                  TAG | ID | LEN | DATA
TData:                   TAG | ID | COUNT | FIELD* | EXTENDS-BLOCK
TList:                   TAG | ID | COUNT | ITEM*
TMap:                    TAG | ID | COUNT | (KEY VALUE)*
```

- **TBool**：`true` / `false` 直接编码在 TAG 的值长度位：`1` = false，`2` = true，其后不跟任何值字节。
- **TBytes**：`LEN` 为数据长度（变长整数），`DATA` 为原始字节。`string`、`[]byte`、`date`（9 字节）、`decimal`（字符串形式）都编码为 TBytes。
- **COUNT**：元素/字段数量，变长整数。

### Data 的 EXTENDS-BLOCK

Data 字段尾部固定跟随一个扩展块，用于继承（extends）场景：

```
无扩展:  TAG(TInt, idLen=0, 1)                         —— 仅 1 字节
有扩展:  TAG(TInt, idLen=1, LEN) | COUNT | EXTEND*
```

`EXTEND` 与普通字段编码相同（`TAG | ID | VALUE`）。字段与扩展均按 ID 升序写入。

### 元素编码（List 元素、Map 的 Key/Value）

元素不带 ID，通过 TAG 的 ID 长度位区分「有值 / 空」：

```
有值:    TAG(类型, idLen=1, 值长度) | VALUE
空/null: TAG(类型, idLen=0, 1)                          —— 无 VALUE
```

解码端以 `idLen == 1` 作为「需要读取值」的判定（`valRead`）。零值元素与 nil 指针元素统一编码为空标记 `TAG(类型, idLen=0, 1)`（无 VALUE），解码时跳过、元素保持零值（TBool 无 VALUE 字节，`false` 元素以 `TAG(TBool, idLen=1, 1)` 正常写入）。唯一特例：

- `uint8` 零值元素编码为 `TAG(TUint, idLen=2, 1)`（`idLen > 0` 触发解码但 `valRead=false`，取值 0）。

### 空值语义

- **结构体字段**：非指针字段为零值（`0` / `""` / `false` / 空集合 / 零时间 / 零 decimal）时不写入；指针字段为 `nil` 时不写入，非 nil 即写入（即使指向零值，如 `false`、`""`、空集合）。解码时未出现的字段保持零值。唯一例外：`*uint8` 指向 `0` 时仍视为空、不写入。
- **List 元素 / Map 的 Key/Value**：空值写 1 字节 TAG（`idLen=0`）标记，保证元素数量对齐（特例见「元素编码」一节）。

### 时间编码（date）

`time.Time` 编码为 `TBytes` 承载的固定 9 字节小端整数：**42 位秒 + 30 位纳秒**（`EncodeTime` / `DecodeTime`）。

## 解码规则

- **跨类型兼容**：数值类描述符（int/uint/float/decimal）均可接受 `TBool / TInt / TUint / TFloat / TBytes` 输入并转换（如 int64 字段可接受字符串数值）；`string` / `[]byte` 字段可接受数值、浮点、布尔输入并转为其文本形式。字段类型演进（如 int8 → int64）无需重建数据。
- **未知字段跳过**：遇到目标结构中不存在的 ID，按类型走 `SkipValue` 跳过，保证向后兼容。`SkipValue` 按实际编码约定处理：字段为 `TAG | ID | VALUE`；List 元素 / Map 的 Key/Value 为 `TAG | VALUE`（无 ID 字节），`idLen == 1` 表示元素有值，才继续跳过其值部分。

## API 与使用方式

```go
package hbuf

// Data 接口：数据对象需实现 Descriptors() 返回其描述符
type Data interface {
    Descriptors() Descriptor
}

func Marshal(data Data) (b []byte, err error)
func Unmarshal(buf []byte, data Data) error
```

典型用法（由 `.hbuf` IDL 代码生成，见 [hbuf_test.go](hbuf_test.go)）：

```go
package main

import (
    "reflect"
    "unsafe"

    "github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
)

type User struct {
    ID   int64  `hbuf:"1"`
    Name string `hbuf:"2"`
}

var userDescriptor = hbuf.NewDataDescriptor(
    0, false, reflect.TypeOf(&User{}),
    nil, // extendMap：无继承时传 nil
    map[uint16]hbuf.Descriptor{
        1: hbuf.NewInt64Descriptor(unsafe.Offsetof(User{}.ID), false),
        2: hbuf.NewStringDescriptor(unsafe.Offsetof(User{}.Name), false),
    })

func (u *User) Descriptors() hbuf.Descriptor { return userDescriptor }
```

可用的 Descriptor 构造器（descriptor.go）：

- `NewDataDescriptor` / `NewRefDescriptor`（结构体 / 结构体引用，`RefDescriptor` 通过 `sync.Once` 延迟解析，支持递归结构）
- `NewListDescriptor[T]` / `NewMapDescriptor[K, V]`（泛型集合）
- `NewInt8~64Descriptor` / `NewUint8~64Descriptor` / `NewFloatDescriptor` / `NewDoubleDescriptor`
- `NewBoolDescriptor` / `NewStringDescriptor` / `NewBytesDescriptor`
- `NewTimeDescriptor` / `NewDecimalDescriptor`
- `NewSyncDescriptor`（延迟构建描述符）

## IDL 语法

见 [hbuf_test.hbuf](hbuf_test.hbuf)（节选，`HBufTest` 完整定义含 128 个字段），支持：`?`（可空，映射为指针）、`[]`（列表）、`value<key>`（Map）、`:`（继承，可多继承并指定扩展 ID）：

```
package go = "hbuf"

data HBufSubTest {
    int64 A1 = 1
    int64? A2 = 2
}

data HBufTest {
    int8 V1 = 1
    int8? V2 = 2
    bool V17 = 17
    string V19 = 19
    bytes V21 = 21
    float V23 = 23
    double V25 = 25
    date V27 = 27
    decimal V29 = 29
    HBufSubTest V31 = 31
    int64[] v39 = 39
    int8<int64> v65 = 65
}

data HBufBTest {
    int64 C1 = 1
    int64? C2 = 2
}

data HBufSub1Test:HBufBTest = 1, HBufSubTest = 2, HBufTest = 3 {
    int64 V1 = 1
    int64? V2 = 2
}
```

IDL 类型与 Go 的对应：`int8`→`int8`、`bytes`→`[]byte`、`float`/`double`→`float32`/`float64`、`date`→`time.Time`、`decimal`→`shopspring.Decimal`、`value<key>`→`map[key]value`。

## 性能对比

测试对象：128 个字段、覆盖全部类型的 `HBufTest` 与等价 `ProtoTest`，字段值取各类型边界（整数取 min / max，字符串含空串与 304 字节长串，bytes 含 0x00~0xFF 全字节序列，时间含 9999 年上限，decimal 含大数与极小指数），两侧语义一致（对比基准见 [data_test.go](data_test.go) 的 `TestEncoderDecoder` / `BenchmarkEncoderDecoder`）。

### 序列化体积（字节）

```text
EncoderProto len: 18062
EncoderJson  len: 21052
EncoderHBuf  len: 16942
```

HBuf 体积约为 Protobuf 的 **94%**、JSON 的 **80%**。

### 编码 / 解码耗时

测试环境：goos: darwin, goarch: amd64, Go 1.27.3, CPU: Intel(R) Core(TM) i9-9980HK @ 2.40GHz, 16 线程（2026-10-21 实测）。

```text
BenchmarkEncoderDecoder/EncoderProto-16       15032      79508 ns/op
BenchmarkEncoderDecoder/DecoderProto-16       14029      87480 ns/op
BenchmarkEncoderDecoder/EncoderJson-16        12448      96970 ns/op
BenchmarkEncoderDecoder/DecoderJson-16         7851     150316 ns/op
BenchmarkEncoderDecoder/EncoderHBuf-16        20284      60205 ns/op
BenchmarkEncoderDecoder/DecoderHBuf-16        21026      58539 ns/op
```

- 编码：HBuf 60 µs，约为 Proto（80 µs）的 **76%**、Json（97 µs）的 **62%**。
- 解码：HBuf 59 µs，约为 Proto（87 µs）的 **67%**、Json（150 µs）的 **39%**。

复现命令：

```shell
go test -run TestEncoderDecoder -v ./pkg/hbuf/       # 体积对比
go test -bench BenchmarkEncoderDecoder -benchmem ./pkg/hbuf/   # 性能对比
```

## 跨语言扩展指南

### 现状与分工

- **协议内核可整体移植**：[encoder.go](encoder.go) / [decoder.go](decoder.go) 是纯函数实现（变长整数、TAG/ID、时间位域拼接），无 unsafe、无 Go 特性依赖，可逐函数翻译到任意语言。
- **unsafe 仅存在于字段访问层**：[descriptor.go](descriptor.go) 用 `unsafe.Offsetof` 直读写字段，其他语言需用各自机制替代（C/C++ 宏 offsetof、Rust `repr(C)`、安全语言则退化为逐字段编解码调用）。
- **协议规则部分散落在 Go 实现中**：扩展前必须冻结为语言无关规范（见下）。

### 第一步：冻结协议规范（需先决策的问题）

以下事项目前由 Go 实现隐式定义，跨语言前必须定案：

| 议题 | Go 现状 | 跨语言需决策 |
|---|---|---|
| map 元素输出顺序 | `for range` 随机序，字节级不确定 | 按 key 编码字节序升序输出（需同步修改 Go 实现）；或 conformance 只做语义比对 |
| 时区语义 | 解码返回 `.In(time.Local)` | 建议规范为 UTC 语义，避免跨时区机器解码结果漂移 |
| 负时间 | `uint64(t.Unix())` 溢出，不支持 | 明确约束：秒必须非负，编码前校验 |
| decimal 精度 | shopspring 任意精度 | 各语言精度不同（Python `Decimal` 默认 28 位上下文、JS 无原生 decimal），需规定精度与舍入行为 |
| 跨类型兼容解码 | 各 Descriptor 的 `Decode` 内 switch-case | 整理成显式转换表，作为各语言实现基准 |

同时，以下隐性规则需精确到字节写入规范（当前见「二进制格式」章节）：TAG 位布局、有符号整数 `LengthInt` 判定与符号扩展、TBool 值在 TAG 位内、uint8 零值元素 `idLen=2`、指针字段非 nil 即写入（`*uint8` 指向 0 除外）、字段按 ID 升序输出、Data 的 EXTENDS-BLOCK 终止标记、空值语义（零值省略 / 元素 `idLen=0` 标记）。

### 第二步：代码生成器多后端

生成器需重构为 **IR（中间表示）→ 后端模板**架构，每目标语言一个后端 + 类型映射表：

| HBuf 类型 | Go | Java | Python | TS/Node |
|---|---|---|---|---|
| int8 | int8 | byte | int | number |
| bytes | []byte | ByteString | bytes | Uint8Array |
| time | time.Time | Instant | datetime（⚠️微秒精度） | Date（⚠️毫秒精度） |
| decimal | shopspring.Decimal | BigDecimal | Decimal（⚠️上下文精度） | decimal.js |
| 可空 `?` | *T | nullable wrapper | Optional/None | null/undefined |

生成策略因语言而异：具备 offsetof 能力的语言（C/C++/Rust）可复刻零拷贝 descriptor；安全语言（Java/C#/Python/TS）生成逐字段 codec 调用，性能模型不同。

### 第三步：各语言运行时库核心组件

每个语言都要实现同一套最小内核：

1. 变长整数编解码（有符号 + 符号扩展、无符号 1~8 字节小端）；
2. TAG / ID 读写（位操作 + 全部特例）；
3. 各标量 codec（bool 位内编码、float IEEE-754 位模式）；
4. 时间 42+30 位、decimal 字符串承载；
5. Data / List / Map 容器 + `SkipValue`（未知字段跳过，最易出错）；
6. 跨类型兼容解码转换表。

语言特有坑：时间精度（Python 微秒、JS 毫秒会丢纳秒）、nil 语义映射（Go nil ↔ nullable ↔ None ↔ undefined）、时区（编码存绝对时间秒+纳秒，解码语义需统一）。

### 第四步：跨语言 conformance 测试

- **golden 字节向量**：128 字段边界值测试数据（全类型 min/max、零值、空值、长串、全字节序列，见 [data_test.go](data_test.go)）即可导出为 golden file，各语言实现必须产出相同字节；
- **矩阵互测**：语言 A 编码 → 语言 B 解码 → 语义相等，用 docker + CI 矩阵自动化（参照 protobuf-conformance 套件路线）。

### 第五步：RPC 层与生态

- hrpc 的传输与 HBuf body 解耦（HTTP/HTTPS/WS/WSS/gRPC），其他语言只需「HTTP client + HBuf codec」即可与 Go 服务互通，生成器顺带产出 client stub；
- 可选：buf 风格的 lint / breaking-change 检测、IDL 版本发布机制。

### 建议路线图

| 阶段 | 内容 | 说明 |
|---|---|---|
| ① | 规范冻结 + map 排序定案 + golden 向量 | 成本低；不定案后续全是返工 |
| ② | 一个参考实现（Java 或 TypeScript） | 用户群最大；用 golden 向量验证规范完整性 |
| ③ | 生成器多后端 + CI conformance 矩阵 | 复制到更多语言 |
| ④ | RPC stub、工具链、文档 | 生态完善 |

核心结论：工程量不在翻译代码（协议内核很小），而在把 Go 实现的隐性规则冻结成规范，并用 conformance 测试守住跨语言一致性——尤其 map 顺序、时区语义、decimal 精度三个问题需最先决策。
```