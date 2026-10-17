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

> 实现：`WriterType` / `DecodeType`（descriptor.go / decoder.go）。

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

解码端以 `idLen == 1` 作为「需要读取值」的判定（`valRead`）。「空 / null」有两个编码特例（同样无 VALUE）：

- `uint8` 零值元素编码为 `TAG(TUint, idLen=2, 1)`；
- `int32` 指针元素为 `nil` 时编码为 `TAG(TInt, idLen=0, 值长度位=7)`（`Int32Descriptor` 传入 `WriterType(buf, TInt, 0, 0)`，解码 valueLen=8）。

### 空值语义

- **结构体字段**：值为零值（`0` / `""` / `false` / 空集合 / 零时间 / 零 decimal）或指针为 `nil` 时，整个字段不写入；解码时未出现的字段保持零值。例外：`*bool` 指针只要非 nil 就会写入（包括 `false`）。
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

见 [hbuf_test.hbuf](hbuf_test.hbuf)，支持：`?`（可空，映射为指针）、`[]`（列表）、`value<key>`（Map）、`:`（继承，可多继承并指定扩展 ID）：

```
package go = "hbuf"

data HBufSubTest {
    int64 A1 = 1
    int64? A2 = 2
}

data HBufTest {
    string V19 = 19
    int64[] v39 = 39
    int8<int64> v65 = 65
    HBufSubTest V31 = 31
}

data HBufSub1Test: HBufBTest = 1, HBufSubTest = 2, HBufTest = 3 {
    int64 V1 = 1
    int64? V2 = 2
}
```

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

测试环境：goos: darwin, goarch: amd64, Go 1.27.0, CPU: Intel(R) Core(TM) i9-9980HK @ 2.40GHz, 16 线程（2026-10-17 实测）。

```text
BenchmarkEncoderDecoder/EncoderProto-16       15830      75252 ns/op
BenchmarkEncoderDecoder/DecoderProto-16       14644      82040 ns/op
BenchmarkEncoderDecoder/EncoderJson-16        13462      89953 ns/op
BenchmarkEncoderDecoder/DecoderJson-16         8334     144243 ns/op
BenchmarkEncoderDecoder/EncoderHBuf-16        22377      53454 ns/op
BenchmarkEncoderDecoder/DecoderHBuf-16        22000      54401 ns/op
```

- 编码：HBuf 53 µs，约为 Proto（75 µs）的 **71%**、Json（90 µs）的 **59%**。
- 解码：HBuf 54 µs，约为 Proto（82 µs）的 **66%**、Json（144 µs）的 **38%**。

复现命令：

```shell
go test -run TestEncoderDecoder -v ./pkg/hbuf/       # 体积对比
go test -bench BenchmarkEncoderDecoder -benchmem ./pkg/hbuf/   # 性能对比
```
