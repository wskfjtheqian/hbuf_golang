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
)

// newH2FramerServer 启动自研 H2FramerServer 服务端,返回监听地址。
// 与 newH2cServer 同参数形态,便于对照测试互换服务端。
func newH2FramerServer(tb testing.TB, rpcServer *hrpc.Server) string {
	tb.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		tb.Fatal(err)
	}
	h2srv := hrpc.NewH2Server("/rpc/", rpcServer)
	go func() { _ = h2srv.Serve(listener) }()
	tb.Cleanup(func() {
		_ = listener.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = h2srv.Shutdown(ctx)
	})
	return listener.Addr().String()
}

// 测试 H2FramerServer 的基本调用与流式回显(客户端用 H2FramerTransport)。
func TestH2FramerServer_Basic(t *testing.T) {
	rpcServer := hrpc.NewServer()
	RegisterHbufService(rpcServer, &TestHbufService{})
	addr := newH2FramerServer(t, rpcServer)

	transport := hrpc.NewH2FramerTransport()
	defer transport.CloseIdleConnections()
	client := hrpc.NewHttpClient("http://"+addr+"/rpc", hrpc.WithH2FramerTransport(transport))
	testClient := NewHbufServiceClient(hrpc.NewClient(client.Request))

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

// 测试 H2FramerServer 的大响应流控:服务端发送窗口按客户端回补推进。
func TestH2FramerServer_LargePayload(t *testing.T) {
	rpcServer := hrpc.NewServer()
	RegisterHbufService(rpcServer, &TestHbufService{})
	addr := newH2FramerServer(t, rpcServer)

	transport := hrpc.NewH2FramerTransport()
	defer transport.CloseIdleConnections()
	client := hrpc.NewHttpClient("http://"+addr+"/rpc", hrpc.WithH2FramerTransport(transport))
	testClient := NewHbufServiceClient(hrpc.NewClient(client.Request))

	// 256KB 回显:请求方向受服务端 1MB 接收窗口保护,
	// 响应方向受客户端流窗口限制,服务端需分帧并等待 WINDOW_UPDATE。
	payload := bytes.Repeat([]byte("B"), 256<<10)
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

// 测试 H2FramerServer 的取消传播:客户端 ctx 超时发 RST_STREAM,
// 服务端取消 handler 上下文,且连接可继续复用。
func TestH2FramerServer_Cancel(t *testing.T) {
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
	addr := newH2FramerServer(t, rpcServer)

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

// 测试 H2FramerServer 的响应帧合并:服务端侧统计 TCP 写次数,
// 验证写循环批量合并(与客户端 BatchWrite 镜像验证)。
func TestH2FramerServer_BatchWrite(t *testing.T) {
	rpcServer := hrpc.NewServer()
	RegisterHbufService(rpcServer, &TestHbufService{})

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	writes := &atomic.Int64{}
	h2srv := hrpc.NewH2Server("/rpc/", rpcServer)
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go h2srv.ServeConn(&countingConn{Conn: conn, writes: writes})
		}
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = h2srv.Shutdown(ctx)
	})

	transport := hrpc.NewH2FramerTransport()
	defer transport.CloseIdleConnections()
	client := hrpc.NewHttpClient("http://"+listener.Addr().String()+"/rpc", hrpc.WithH2FramerTransport(transport))
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
		t.Fatalf("server frame batching ineffective: %d writes for %d requests, want < %d", got, n, n)
	}
	t.Logf("%d requests merged into %d TCP writes (server side)", n, writes.Load())
}
