package rpc

HRPC 和 HTTP，GRPC 协议的性能对比

```text
cpu: 12th Gen Intel(R) Core(TM) i5-12400
Benchmark_HRPC_HTTP
Benchmark_HRPC_HTTP/HRPC_HTTP_HBufEncode
Benchmark_HRPC_HTTP/HRPC_HTTP_HBufEncode-12         	   19248	     61437 ns/op
Benchmark_HRPC_HTTP/HRPC_HTTP
Benchmark_HRPC_HTTP/HRPC_HTTP-12                    	   18450	     65758 ns/op
Benchmark_HRPC_HTTP/HRPC_HTTPS
Benchmark_HRPC_HTTP/HRPC_HTTPS-12                   	   12978	     96648 ns/op
Benchmark_HRPC_HTTP/HRPC_WS
Benchmark_HRPC_HTTP/HRPC_WS-12                      	   17193	     68161 ns/op
Benchmark_HRPC_HTTP/HRPC_WSS
Benchmark_HRPC_HTTP/HRPC_WSS-12                     	   16544	     70637 ns/op
Benchmark_HRPC_HTTP/Http
Benchmark_HRPC_HTTP/Http-12                         	   21285	     56085 ns/op
Benchmark_HRPC_HTTP/Https
Benchmark_HRPC_HTTP/Https-12                        	   14214	     83532 ns/op
Benchmark_HRPC_HTTP/GRPC
Benchmark_HRPC_HTTP/GRPC-12                         	    9921	    114093 ns/op
PASS
```
