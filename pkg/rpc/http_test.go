package rpc

import (
	"bytes"
	"context"
	"encoding/base64"
	hbuf "github.com/wskfjtheqian/hbuf_golang/pkg/buf"
	"io"
	"net/http"
	"testing"
)

// 测试 HttpService 的 Response 方法
func TestHttpService_Invoke(t *testing.T) {
	rpcServer := NewServer()
	RegisterRpcServer(rpcServer, &TestRpcServer{})

	server := NewHttpServer("/rpc/", rpcServer)

	http.Handle("/rpc/", server)
	go http.ListenAndServe(":8080", nil)

	client := NewHttpClient("http://localhost:8080/rpc")

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

// 测试 HttpService 的 Response 方法
func TestHttpService_InvokeHBuf(t *testing.T) {
	rpcServer := NewServer(WithServerEncoder(NewHBufEncode()), WithServerDecode(NewHBufDecode()))
	RegisterRpcServer(rpcServer, &TestRpcServer{})

	server := NewHttpServer("/rpc/", rpcServer)

	http.Handle("/rpc/", server)
	go http.ListenAndServe(":8080", nil)

	client := NewHttpClient("http://localhost:8080/rpc")

	rpcClient := NewClient(client.Request, WithClientEncoder(NewHBufEncode()), WithClientDecode(NewHBufDecode()))
	testClient := NewTestRpcClient(rpcClient)
	resp, err := testClient.GetName(context.Background(), &GetNameRequest{Name: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Name != "test" {
		t.Fatal("test fail")
	}
}

// 测试 HttpService 加密通信
func TestHttpService_Encoder(t *testing.T) {
	rpcServer := NewServer(WithServerEncoder(NewHBufEncode()), WithServerDecode(NewHBufDecode()))
	RegisterRpcServer(rpcServer, &TestRpcServer{})

	server := NewHttpServer("/rpc/", rpcServer, WithResponseMiddleware(func(next Response) Response {
		return func(ctx context.Context, path string, writer io.Writer, reader io.Reader) error {
			decoder := base64.NewDecoder(base64.StdEncoding, reader)

			encoder := base64.NewEncoder(base64.StdEncoding, writer)
			defer encoder.Close()

			return next(ctx, path, encoder, decoder)
		}
	}))

	http.Handle("/rpc/", server)
	go http.ListenAndServe(":8080", nil)

	client := NewHttpClient("http://localhost:8080/rpc", WithRequestMiddleware(func(next Request) Request {
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
	}))

	rpcClient := NewClient(client.Request, WithClientEncoder(NewHBufEncode()), WithClientDecode(NewHBufDecode()))
	testClient := NewTestRpcClient(rpcClient)
	resp, err := testClient.GetName(context.Background(), &GetNameRequest{Name: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Name != "test" {
		t.Fatal("test fail")
	}
}

// 测试 base64
func TestBase64(t *testing.T) {
	writer := bytes.NewBuffer(nil)
	encoder := base64.NewEncoder(base64.StdEncoding, writer)
	defer encoder.Close()

	encoder.Write([]byte("adfasdfasdfasdfsa"))
}

// 测试 HttpService 的 Middleware 方法
func TestHttpService_Middleware(t *testing.T) {
	rpcServer := NewServer(WithServerMiddleware(func(next Handler) Handler {
		return func(ctx context.Context, req hbuf.Data) (hbuf.Data, error) {
			return next(ctx, req)
		}
	}))
	RegisterRpcServer(rpcServer, &TestRpcServer{})

	server := NewHttpServer("/rpc/", rpcServer)

	http.Handle("/rpc/", server)
	go http.ListenAndServe(":8080", nil)

	client := NewHttpClient("http://localhost:8080/rpc")

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
