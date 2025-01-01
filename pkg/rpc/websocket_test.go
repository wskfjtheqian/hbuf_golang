package rpc

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"
)

// 测试 TestWebsocket_RPC 方法
func TestWebsocket_RPC(t *testing.T) {
	rpcServer := NewServer()
	RegisterRpcServer(rpcServer, &TestRpcServer{})

	server := NewWebSocketServer(rpcServer.Response)

	http.Handle("/rpc", server)
	go http.ListenAndServe(":8080", nil)

	client := NewWebSocketClient("ws://localhost:8080/rpc", nil)

	err := client.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	rpcClient := NewClient(client.Request)
	testClient := NewTestRpcClient(rpcClient)

	resp, err := testClient.GetName(context.Background(), &GetNameRequest{Name: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Name != "test" {
		t.Fatal("test fail")
	}
}

// 测试多个 RPC 同时调用
func TestWebsocket_MultipleRPC(t *testing.T) {
	rpcServer := NewServer()
	RegisterRpcServer(rpcServer, &TestRpcServer{})

	server := NewWebSocketServer(rpcServer.Response)

	http.Handle("/rpc", server)
	go http.ListenAndServe(":8080", nil)
	<-time.After(time.Second)

	waitGroup := sync.WaitGroup{}
	waitGroup.Add(100)
	for i := 0; i < 100; i++ {
		go func() {
			t.Run("TestWebsocket_RPC", func(t *testing.T) {
				defer waitGroup.Done()
				client := NewWebSocketClient("ws://localhost:8080/rpc", nil)

				err := client.Connect(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				rpcClient := NewClient(client.Request)
				testClient := NewTestRpcClient(rpcClient)

				resp, err := testClient.GetName(context.Background(), &GetNameRequest{Name: "test"})
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

// 测试监听 WebSocket 连接 秋 RPC 服务
func TestWebsocket_Listen(t *testing.T) {
	rpcServer := NewServer()
	RegisterRpcServer(rpcServer, &TestRpcServer{})

	server := NewWebSocketServer(rpcServer.Response)
	go server.ListenAndServe(context.Background(), ":8080")
	<-time.After(time.Second)

	client := NewWebSocketClient("ws://localhost:8080/rpc", nil)

	err := client.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	rpcClient := NewClient(client.Request)
	testClient := NewTestRpcClient(rpcClient)

	resp, err := testClient.GetName(context.Background(), &GetNameRequest{Name: "test"}) //调用 RPC 服务
	if err != nil {
		t.Fatal(err)
	}
	if resp.Name != "test" {
		t.Fatal("test fail")
	}

}
