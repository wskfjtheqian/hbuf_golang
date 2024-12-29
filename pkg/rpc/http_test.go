package rpc

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"net/http"
	"testing"
)

// 测试 JsonService 的 Response 方法
func TestJsonService_Invoke(t *testing.T) {
	rpcServer := NewServer()
	RegisterRpcServer(rpcServer, &TestRpcServer{})

	server := NewHttpServer("/rpc/", rpcServer)

	http.Handle("/rpc/", server)
	go http.ListenAndServe(":8080", nil)

	client := NewHttpClient("http://localhost:8080/rpc")

	rpcClient := NewClient(client.Invoke)
	testClient := NewTestRpcClient(rpcClient)
	resp, err := testClient.GetName(context.Background(), &GetNameRequest{Name: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Name != "test" {
		t.Fatal("test fail")
	}
}

// 测试 JsonService
func TestJsonService_EncInvoke(t *testing.T) {
	rpcServer := NewServer()
	RegisterRpcServer(rpcServer, &TestRpcServer{})

	server := NewHttpServer("/rpc/", rpcServer, WithResponseFilter(func(ctx context.Context, response Response) Response {
		return func(ctx context.Context, writer io.Writer, reader io.Reader) error {
			decoder := base64.NewDecoder(base64.StdEncoding, reader)

			encoder := base64.NewEncoder(base64.StdEncoding, writer)
			defer encoder.Close()

			err := response(ctx, encoder, decoder)
			if err != nil {
				return err
			}
			return nil
		}
	}))

	http.Handle("/rpc/", server)
	go http.ListenAndServe(":8080", nil)

	client := NewHttpClient("http://localhost:8080/rpc", WithRequestFilter(func(ctx context.Context, request Request) Request {
		return func(ctx context.Context, path string, reader io.Reader) (io.ReadCloser, error) {
			buffer := bytes.NewBuffer(nil)
			encoder := base64.NewEncoder(base64.StdEncoding, buffer)
			defer encoder.Close()
			_, err := io.Copy(encoder, reader)
			if err != nil {
				return nil, err
			}

			body, err := request(ctx, path, buffer)
			if err != nil {
				return nil, err
			}
			decoder := base64.NewDecoder(base64.StdEncoding, body)
			return io.NopCloser(decoder), nil
		}
	}))

	rpcClient := NewClient(client.Invoke)
	testClient := NewTestRpcClient(rpcClient)
	resp, err := testClient.GetName(context.Background(), &GetNameRequest{Name: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Name != "test" {
		t.Fatal("test fail")
	}
}

func TestBase64Service_EncInvoke(t *testing.T) {
	writer := bytes.NewBuffer(nil)
	encoder := base64.NewEncoder(base64.StdEncoding, writer)
	defer encoder.Close()

	encoder.Write([]byte("adfasdfasdfasdfsa"))
}
