package hrpc_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hrpc"
	"golang.org/x/net/http2"
)

// newH2cServer 启动一个 h2c 测试服务端,返回监听地址。
// 并发流上限设大,保证高并发对比测试中单连接承载全部并发流。
func newH2cServer(tb testing.TB, rpcServer *hrpc.Server) string {
	tb.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		tb.Fatal(err)
	}
	handler := hrpc.NewHttpServer("/rpc/", rpcServer)
	srv := &http2.Server{MaxConcurrentStreams: 10000}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				srv.ServeConn(conn, &http2.ServeConnOpts{Handler: handler})
			}()
		}
	}()
	tb.Cleanup(func() { _ = listener.Close() })
	return listener.Addr().String()
}

// countingConn 统计底层 TCP 写次数,用于验证帧合并效果。
type countingConn struct {
	net.Conn
	writes *atomic.Int64
}

func (c *countingConn) Write(p []byte) (int, error) {
	c.writes.Add(1)
	return c.Conn.Write(p)
}

// 测试 H2FramerTransport 的基本调用与流式回显
func TestH2FramerTransport_Basic(t *testing.T) {
	rpcServer := hrpc.NewServer()
	RegisterHbufService(rpcServer, &TestHbufService{})
	addr := newH2cServer(t, rpcServer)

	transport := hrpc.NewH2FramerTransport()
	defer transport.CloseIdleConnections()
	client := hrpc.NewHttpClient("http://"+addr+"/rpc", hrpc.WithH2FramerTransport(transport))
	rpcClient := hrpc.NewClient(client.Request)
	testClient := NewHbufServiceClient(rpcClient)

	resp, err := testClient.HbufMethod(context.Background(), &HbufRequest{Name: "test", Age: 18})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Name != "test" || resp.Age != 18 {
		t.Fatalf("resp mismatch: %+v", resp)
	}

	stream, err := testClient.HbufStream(context.Background(), bytes.NewReader([]byte("HbufStream Test")))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	data, err := io.ReadAll(stream)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "HbufStream Test" {
		t.Fatalf("stream mismatch: %s", data)
	}
}

// 测试并发请求的帧合并:每个请求有 HEADERS+DATA 两帧,
// 合并后 TCP 写次数应显著少于请求数。
func TestH2FramerTransport_BatchWrite(t *testing.T) {
	rpcServer := hrpc.NewServer()
	RegisterHbufService(rpcServer, &TestHbufService{})
	addr := newH2cServer(t, rpcServer)

	writes := &atomic.Int64{}
	transport := hrpc.NewH2FramerTransport(hrpc.WithFramerDialContext(
		func(ctx context.Context, network, address string) (net.Conn, error) {
			conn, err := (&net.Dialer{}).DialContext(ctx, network, address)
			if err != nil {
				return nil, err
			}
			return &countingConn{Conn: conn, writes: writes}, nil
		}))
	defer transport.CloseIdleConnections()
	client := hrpc.NewHttpClient("http://"+addr+"/rpc", hrpc.WithH2FramerTransport(transport))
	testClient := NewHbufServiceClient(hrpc.NewClient(client.Request))

	const n = 200
	start := make(chan struct{})
	var wg sync.WaitGroup
	errCh := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			resp, err := testClient.HbufMethod(context.Background(), &HbufRequest{Name: "test", Age: 18})
			if err != nil {
				errCh <- err
				return
			}
			if resp.Name != "test" || resp.Age != 18 {
				errCh <- errors.New("resp mismatch")
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatal(err)
	}

	if got := writes.Load(); got >= int64(n) {
		t.Fatalf("frame batching ineffective: %d writes for %d requests, want < %d", got, n, n)
	}
	t.Logf("%d requests merged into %d TCP writes", n, writes.Load())
}

// 测试超过流控窗口的大请求体:验证 DATA 分帧与 WINDOW_UPDATE 回补。
func TestH2FramerTransport_LargePayload(t *testing.T) {
	rpcServer := hrpc.NewServer()
	RegisterHbufService(rpcServer, &TestHbufService{})
	addr := newH2cServer(t, rpcServer)

	transport := hrpc.NewH2FramerTransport()
	defer transport.CloseIdleConnections()
	client := hrpc.NewHttpClient("http://"+addr+"/rpc", hrpc.WithH2FramerTransport(transport))
	testClient := NewHbufServiceClient(hrpc.NewClient(client.Request))

	// 256KB 超过 64KB 初始流窗口,验证流控等待与 WINDOW_UPDATE 唤醒
	payload := bytes.Repeat([]byte("A"), 256<<10)
	stream, err := testClient.HbufStream(context.Background(), bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	data, err := io.ReadAll(stream)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, payload) {
		t.Fatalf("payload mismatch: got %d bytes, want %d", len(data), len(payload))
	}
}

// 测试 ctx 取消触发 RST_STREAM,且取消后连接仍可复用。
func TestH2FramerTransport_Cancel(t *testing.T) {
	rpcServer := hrpc.NewServer()
	RegisterHbufService(rpcServer, &TestHbufService{})
	rpcServer.Register(0, "slow_service", nil, &hrpc.Method{
		Name: "slow",
		WithContext: func(ctx context.Context) context.Context {
			return ctx
		},
		Handler: func(ctx context.Context, req any) (any, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	})
	addr := newH2cServer(t, rpcServer)

	transport := hrpc.NewH2FramerTransport()
	defer transport.CloseIdleConnections()
	client := hrpc.NewHttpClient("http://"+addr+"/rpc", hrpc.WithH2FramerTransport(transport))
	rpcClient := hrpc.NewClient(client.Request)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, err := rpcClient.Invoke(ctx, 0, "slow_service", "slow", &HbufRequest{}, hrpc.NewResultResponse[*HbufResponse]())
	if err == nil {
		t.Fatal("expected timeout error")
	}

	// 取消后同一连接仍可复用
	testClient := NewHbufServiceClient(rpcClient)
	resp, err := testClient.HbufMethod(context.Background(), &HbufRequest{Name: "ok", Age: 1})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Name != "ok" || resp.Age != 1 {
		t.Fatalf("resp mismatch: %+v", resp)
	}
}
