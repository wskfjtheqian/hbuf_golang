package rpc_test

import (
	"context"
	"encoding/base64"
	"github.com/wskfjtheqian/hbuf_golang/pkg/rpc"
	"io"
	"net/http"
	"sync"
	"testing"
	"time"
)

// 测试 TestWebsocket_RPC 方法
func TestWebsocket_RPC(t *testing.T) {
	rpcServer := rpc.NewServer(rpc.WithServerEncoder(rpc.NewHBufEncode()), rpc.WithServerDecode(rpc.NewHBufDecode()))
	RegisterHbufService(rpcServer, &TestHbufService{})

	server := rpc.NewWebSocketServer(rpcServer.Response)

	http.Handle("/socket", server)
	go http.ListenAndServe(":8080", nil)

	client := rpc.NewWebSocketClient("ws://localhost:8080/socket", nil)

	err := client.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	rpcClient := rpc.NewClient(client.Request, rpc.WithClientEncoder(rpc.NewHBufEncode()), rpc.WithClientDecode(rpc.NewHBufDecode()))
	testClient := NewHbufServiceClient(rpcClient)
	//<-time.After(time.Second * 1)
	resp, err := testClient.HbufMethod(context.Background(), &HbufRequest{Name: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Name != "test" {
		t.Fatal("test fail")
	}
}

// 测试多个 RPC 同时调用
func TestWebsocket_MultipleRPC(t *testing.T) {
	rpcServer := rpc.NewServer(rpc.WithServerEncoder(rpc.NewHBufEncode()), rpc.WithServerDecode(rpc.NewHBufDecode()))
	RegisterHbufService(rpcServer, &TestHbufService{})

	server := rpc.NewWebSocketServer(rpcServer.Response)

	go server.ListenAndServe(context.Background(), ":8080")
	<-time.After(time.Second)

	waitGroup := sync.WaitGroup{}
	waitGroup.Add(100)
	for i := 0; i < 100; i++ {
		go func() {
			t.Run("TestWebsocket_RPC", func(t *testing.T) {
				defer waitGroup.Done()
				client := rpc.NewWebSocketClient("ws://localhost:8080/socket", nil)

				err := client.Connect(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				rpcClient := rpc.NewClient(client.Request, rpc.WithClientEncoder(rpc.NewHBufEncode()), rpc.WithClientDecode(rpc.NewHBufDecode()))
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
	rpcServer := rpc.NewServer()
	RegisterHbufService(rpcServer, &TestHbufService{})

	server := rpc.NewWebSocketServer(rpcServer.Response)
	go server.ListenAndServe(context.Background(), ":8080")
	<-time.After(time.Second)

	client := rpc.NewWebSocketClient("ws://localhost:8080/socket", nil)

	err := client.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	rpcClient := rpc.NewClient(client.Request)
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
	rpcServer := rpc.NewServer()
	RegisterHbufService(rpcServer, &TestHbufService{})

	requestMiddleware := func(next rpc.Request) rpc.Request {
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

	responseMiddleware := func(next rpc.Response) rpc.Response {
		return func(ctx context.Context, path string, writer io.Writer, reader io.Reader) error {
			decoder := base64.NewDecoder(base64.StdEncoding, reader)

			encoder := base64.NewEncoder(base64.StdEncoding, writer)
			defer encoder.Close()

			return next(ctx, path, encoder, decoder)
		}
	}

	server := rpc.NewWebSocketServer(rpcServer.Response,
		rpc.WithWebSocketServerRequestMiddleware(requestMiddleware),
		rpc.WithWebSocketServerResponseMiddleware(responseMiddleware),
	)
	go server.ListenAndServe(context.Background(), ":8080")
	<-time.After(time.Second)

	client := rpc.NewWebSocketClient("ws://localhost:8080/socket", nil,
		rpc.WithWebSocketClientRequestMiddleware(requestMiddleware),
		rpc.WithWebSocketClientResponseMiddleware(responseMiddleware),
	)

	err := client.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	rpcClient := rpc.NewClient(client.Request)
	testClient := NewHbufServiceClient(rpcClient)

	resp, err := testClient.HbufMethod(context.Background(), &HbufRequest{Name: "test"}) //调用 RPC 服务
	if err != nil {
		t.Fatal(err)
	}
	if resp.Name != "test" {
		t.Fatal("test fail")
	}

}
