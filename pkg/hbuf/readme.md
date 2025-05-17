### Field 结构体定义如下：

A.值的长 uint64(01234567 + 1 )  
B.ID长度 uint16(012 + 1)
------------------------------------------------------------------------------------------------------------------------

##### TInt TUint TFloat 的TAG格式

| 类型 Type | 是否是为空 | 值的长 | ID长度 |
|---------|-------|-----|------|
| 111     | 1     | 111 | 1    |

##### TInt TUint TFloat 的Field格式

| TAG | ID | VALUE |

------------------------------------------------------------------------------------------------------------------------

##### TBool 的 TAG

| 类型 Type	 | 是否是为空 | Value	 1(false) or 2(true) | ID长度 |  
|----------|-------|----------------------------|------|
| 111      | 1     | 111                        | 1    |

##### TBool 的Field格式

| TAG | NULL | 0(false) or 1(true) | ID |

------------------------------------------------------------------------------------------------------------------------

##### TBytes 的 TAG

| 类型 Type | 是否是为空 | 值长度len(bytes) | ID长度 |
|---------|-------|---------------|------|
| 111     | 1     | 111           | 1    |

##### TBytes的Field格式

| TAG | ID | LEN | VALUE |

------------------------------------------------------------------------------------------------------------------------

##### Data 的 TAG

| 类型 Type | 是否是为空 | 数据长度的长度 | ID长度 |
|---------|-------|---------|------|
| 111     | 1     | 111     | 1    |

##### Data的Field格式

| TAG | ID | 数据的长度 | Field | Field | ...|

------------------------------------------------------------------------------------------------------------------------

##### LIST 的 TAG

| 类型 Type | 是否是为空 | 数据长度的长度 | ID长度 |
|---------|-------|---------|------|
| 111     | 1     | 111     | 1    |

##### List的Field格式

| TAG | ID | 数据长度的长度｜Item数量 | Item | Item | ...|

------------------------------------------------------------------------------------------------------------------------
ProtoBuf Json HBuf的Encoder 序列化数据长度对比测试结果如下：
```text
=== RUN   TestEncoderDecoder
=== RUN   TestEncoderDecoder/EncoderProto
    data_test.go:290: EncoderProto len: 4109
=== RUN   TestEncoderDecoder/EncoderJson
    data_test.go:300: EncoderJson len: 5760
=== RUN   TestEncoderDecoder/EncoderHBuf
    data_test.go:311: EncoderHBuf len: 3388
```

ProtoBuf Json HBuf的Encoder和Decoder性能对比测试结果如下：
```text
goos: darwin
goarch: amd64
pkg: github.com/wskfjtheqian/hbuf_golang/pkg/hbuf
cpu: Intel(R) Core(TM) i9-9980HK CPU @ 2.40GHz
BenchmarkName
BenchmarkName/EncoderProto
BenchmarkName/EncoderProto-16         	   12500	     93101 ns/op
BenchmarkName/DecoderProto
BenchmarkName/DecoderProto-16         	    9937	    109532 ns/op
BenchmarkName/EncoderJson
BenchmarkName/EncoderJson-16          	   12254	     96551 ns/op
BenchmarkName/DecoderJson
BenchmarkName/DecoderJson-16          	    5379	    220053 ns/op
BenchmarkName/EncoderHBuf
BenchmarkName/EncoderHBuf-16          	   24936	     45349 ns/op
BenchmarkName/DecoderHBuf
BenchmarkName/DecoderHBuf-16          	   16478	     71639 ns/op
```
