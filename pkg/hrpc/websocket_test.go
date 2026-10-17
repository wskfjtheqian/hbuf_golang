package hrpc_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hrpc"
)

// 测试 TestWebsocket_RPC 方法
func TestWebsocket_RPC(t *testing.T) {
	rpcServer := hrpc.NewServer(hrpc.WithServerEncoder(hrpc.NewHBufEncode()), hrpc.WithServerDecode(hrpc.NewHBufDecode()))
	RegisterHbufService(rpcServer, &TestHbufService{})

	server := hrpc.NewWebSocketServer(rpcServer.Response, hrpc.WithWebSocketServerEncode(hrpc.NewHBufEncode()), hrpc.WithWebSocketServerDecode(hrpc.NewHBufDecode()))

	http.HandleFunc("/socket", func(writer http.ResponseWriter, request *http.Request) {
		server.ServeHTTP(writer, request, "", nil, nil)
	})
	go http.ListenAndServe(":8080", nil)

	client := hrpc.NewWebSocketClient("ws://localhost:8080/socket", nil, hrpc.WithWebSocketClientEncode(hrpc.NewHBufEncode()), hrpc.WithWebSocketClientDecode(hrpc.NewHBufDecode()))

	err := client.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	rpcClient := hrpc.NewClient(client.Request, hrpc.WithClientEncoder(hrpc.NewHBufEncode()), hrpc.WithClientDecode(hrpc.NewHBufDecode()))
	testClient := NewHbufServiceClient(rpcClient)
	//<-time.After(time.Second * 1)
	resp, err := testClient.HbufMethod(context.Background(), &HbufRequest{Name: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Name != "test" {
		t.Fatal("test fail")
	}

	stream, err := testClient.HbufStream(context.Background(), bytes.NewReader([]byte("HbufStream Test")))
	if err != nil {
		t.Error(err)
		return
	}
	defer stream.Close()
	data, err := io.ReadAll(stream)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "HbufStream Test" {
		t.Fatal("test fail")
	}
}

// 测试多个 RPC 同时调用
func TestWebsocket_MultipleRPC(t *testing.T) {
	rpcServer := hrpc.NewServer(hrpc.WithServerEncoder(hrpc.NewHBufEncode()), hrpc.WithServerDecode(hrpc.NewHBufDecode()))
	RegisterHbufService(rpcServer, &TestHbufService{})

	server := hrpc.NewWebSocketServer(rpcServer.Response, hrpc.WithWebSocketServerEncode(hrpc.NewHBufEncode()), hrpc.WithWebSocketServerDecode(hrpc.NewHBufDecode()))

	go server.ListenAndServe(context.Background(), ":8080")
	<-time.After(time.Second)

	waitGroup := sync.WaitGroup{}
	waitGroup.Add(100)
	for i := 0; i < 100; i++ {
		go func() {
			t.Run("TestWebsocket_RPC", func(t *testing.T) {
				defer waitGroup.Done()
				client := hrpc.NewWebSocketClient("ws://localhost:8080/socket", nil, hrpc.WithWebSocketClientEncode(hrpc.NewHBufEncode()), hrpc.WithWebSocketClientDecode(hrpc.NewHBufDecode()))

				err := client.Connect(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				rpcClient := hrpc.NewClient(client.Request, hrpc.WithClientEncoder(hrpc.NewHBufEncode()), hrpc.WithClientDecode(hrpc.NewHBufDecode()))
				testClient := NewHbufServiceClient(rpcClient)

				resp, err := testClient.HbufMethod(context.Background(), &HbufRequest{Name: "test"})
				if err != nil {
					t.Fatal(err)
				}
				if resp.Name != "test" {
					t.Fatal("test fail")
				}
			})
		}()
	}
	waitGroup.Wait()
}

// 测试监听 WebSocket 连接 RPC 服务
func TestWebsocket_Listen(t *testing.T) {
	rpcServer := hrpc.NewServer()
	RegisterHbufService(rpcServer, &TestHbufService{})

	server := hrpc.NewWebSocketServer(rpcServer.Response)
	go server.ListenAndServe(context.Background(), ":8080")
	<-time.After(time.Second)

	client := hrpc.NewWebSocketClient("ws://localhost:8080/socket", nil)

	err := client.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	rpcClient := hrpc.NewClient(client.Request)
	testClient := NewHbufServiceClient(rpcClient)

	resp, err := testClient.HbufMethod(context.Background(), &HbufRequest{Name: "test"}) //调用 RPC 服务
	if err != nil {
		t.Fatal(err)
	}
	if resp.Name != "test" {
		t.Fatal("test fail")
	}

}

// 测试 TestWebsocket 加密通信
func TestWebsocket_Encrypt(t *testing.T) {
	rpcServer := hrpc.NewServer()
	RegisterHbufService(rpcServer, &TestHbufService{})

	requestMiddleware := func(next hrpc.Request) hrpc.Request {
		return func(ctx context.Context, path string, notification bool, callback func(writer io.Writer) error) (io.ReadCloser, error) {
			body, err := next(ctx, path, notification, func(writer io.Writer) error {
				encoder := base64.NewEncoder(base64.StdEncoding, writer)
				defer encoder.Close()

				return callback(encoder)
			})

			if err != nil {
				return nil, err
			}
			decoder := base64.NewDecoder(base64.StdEncoding, body)
			return io.NopCloser(decoder), nil
		}
	}

	responseMiddleware := func(next hrpc.Response) hrpc.Response {
		return func(ctx context.Context, path string, writer io.Writer, reader io.Reader, header http.Header) error {
			decoder := base64.NewDecoder(base64.StdEncoding, reader)

			encoder := base64.NewEncoder(base64.StdEncoding, writer)
			defer encoder.Close()

			return next(ctx, path, encoder, decoder, header)
		}
	}

	server := hrpc.NewWebSocketServer(rpcServer.Response,
		hrpc.WithWebSocketServerRequestMiddleware(requestMiddleware),
		hrpc.WithWebSocketServerResponseMiddleware(responseMiddleware),
	)
	go server.ListenAndServe(context.Background(), ":8080")
	<-time.After(time.Second)

	client := hrpc.NewWebSocketClient("ws://localhost:8080/socket", nil,
		hrpc.WithWebSocketClientRequestMiddleware(requestMiddleware),
		hrpc.WithWebSocketClientResponseMiddleware(responseMiddleware),
	)

	err := client.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	rpcClient := hrpc.NewClient(client.Request)
	testClient := NewHbufServiceClient(rpcClient)

	resp, err := testClient.HbufMethod(context.Background(), &HbufRequest{Name: "test"}) //调用 RPC 服务
	if err != nil {
		t.Fatal(err)
	}
	if resp.Name != "test" {
		t.Fatal("test fail")
	}

	stream, err := testClient.HbufStream(context.Background(), bytes.NewReader([]byte("HbufStream Test")))
	if err != nil {
		t.Error(err)
		return
	}
	defer stream.Close()
	data, err := io.ReadAll(stream)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "HbufStream Test" {
		t.Fatal("test fail")
	}

}

// 测试 traceId 经 WebSocket 透传到服务端 handler
func TestWebsocket_TraceId(t *testing.T) {
	traceIds := make(chan string, 1)
	rpcServer := hrpc.NewServer()
	rpcServer.Register(0, "test", nil, &hrpc.Method{
		Name: "trace",
		WithContext: func(ctx context.Context) context.Context {
			return ctx
		},
		Handler: func(ctx context.Context, req any) (any, error) {
			traceIds <- hlog.FromContext(ctx)
			return &HbufResponse{}, nil
		},
	})

	server := hrpc.NewWebSocketServer(rpcServer.Response)
	go server.ListenAndServe(context.Background(), ":8085")
	<-time.After(time.Second)

	client := hrpc.NewWebSocketClient("ws://localhost:8085/socket", nil)
	err := client.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	rpcClient := hrpc.NewClient(client.Request)

	traceId := "trace-test-123"
	ctx := hlog.WithContext(context.Background(), traceId)
	_, err = rpcClient.Invoke(ctx, 0, "test", "trace", bytes.NewReader([]byte("{}")), hrpc.NewResultResponse[*HbufResponse]())
	if err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-traceIds:
		if got != traceId {
			t.Fatalf("traceId mismatch: got %s, want %s", got, traceId)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("handler not called")
	}
}

// 测试连接关闭后发送返回错误
func TestWebsocket_SendAfterClose(t *testing.T) {
	server := hrpc.NewWebSocketServer(nil)

	http.HandleFunc("/socket-close", func(writer http.ResponseWriter, request *http.Request) {
		server.ServeHTTP(writer, request, "id-close", nil, nil)
	})
	go http.ListenAndServe(":8086", nil)
	<-time.After(time.Second)

	client := hrpc.NewWebSocketClient("ws://localhost:8086/socket-close", nil)
	err := client.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	<-time.After(500 * time.Millisecond) // 等待服务端注册连接

	server.CloseClient(context.Background(), "id-close")
	<-time.After(500 * time.Millisecond) // 等待读 goroutine 退出并关闭 done

	_, err = client.Request(context.Background(), "/test", true, func(writer io.Writer) error {
		_, err := writer.Write([]byte("{}"))
		return err
	})
	if err == nil {
		t.Fatal("expected error after close")
	}
}

// 测试并发发送与连接关闭的竞态,确保无 send on closed channel panic
func TestWebsocket_CloseRace(t *testing.T) {
	server := hrpc.NewWebSocketServer(nil)

	http.HandleFunc("/socket-race", func(writer http.ResponseWriter, request *http.Request) {
		server.ServeHTTP(writer, request, "id-race", nil, nil)
	})
	go http.ListenAndServe(":8087", nil)
	<-time.After(time.Second)

	for i := 0; i < 20; i++ {
		client := hrpc.NewWebSocketClient("ws://localhost:8087/socket-race", nil)
		if err := client.Connect(context.Background()); err != nil {
			t.Fatal(err)
		}
		<-time.After(300 * time.Millisecond) // 等待服务端注册连接

		waitGroup := sync.WaitGroup{}
		waitGroup.Add(10)
		for j := 0; j < 10; j++ {
			go func() {
				defer waitGroup.Done()
				for k := 0; k < 50; k++ {
					// 发送 notification,关闭前后返回错误均属预期,关键在于不 panic
					_, _ = client.Request(context.Background(), "/test", true, func(writer io.Writer) error {
						_, err := writer.Write([]byte("{}"))
						return err
					})
				}
			}()
		}
		server.CloseClient(context.Background(), "id-race")
		waitGroup.Wait()
	}
}
