package rpc

import (
	"context"
	"net/http"
	"testing"
)

// 测试 JsonService 的 Response 方法
func TestWebsocketService_Invoke(t *testing.T) {
	rpcServer := NewServer()
	RegisterRpcServer(rpcServer, &TestRpcServer{})

	server := NewWebSocketServer(0, rpcServer.Response)

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
