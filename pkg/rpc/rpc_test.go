package rpc

import (
	"context"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"io"
	"net/http"
	"testing"
)

type GetNameRequest struct {
	Name string `json:"name"`
}

func (r *GetNameRequest) Data() hbuf.Data {
	return hbuf.Data(r)
}

func (g GetNameRequest) Encoder(w io.Writer) (err error) {
	return nil
}

func (g GetNameRequest) Decoder(r io.Reader) (err error) {
	return nil
}

type GetNameResponse struct {
	Name string `json:"name"`
}

func (r *GetNameResponse) Data() hbuf.Data {
	return hbuf.Data(r)
}
func (g GetNameResponse) Encoder(w io.Writer) (err error) {
	return nil
}

func (g GetNameResponse) Decoder(r io.Reader) (err error) {
	return nil
}

type TestRpc interface {
	GetName(ctx context.Context, req *GetNameRequest) (*GetNameResponse, error)
}

func NewTestRpcClient(client *Client) TestRpc {
	return &TestRpcClient{
		client: client,
	}
}

type TestRpcClient struct {
	client *Client
}

func (t TestRpcClient) GetName(ctx context.Context, req *GetNameRequest) (*GetNameResponse, error) {
	response, err := ClientCall[GetNameRequest, GetNameResponse](ctx, t.client, 0, "TestRpc", "GetName", req)
	if err != nil {
		return nil, err
	}
	return &response, nil
}

func RegisterRpcServer(r *Server, server TestRpc) {
	r.Register(0, "TestRpc",
		&MethodImpl[GetNameRequest, GetNameResponse]{
			Name: "GetName",
			Handler: func(ctx context.Context, req hbuf.Data) (hbuf.Data, error) {
				return server.GetName(ctx, (req).(*GetNameRequest))
			},
		},
	)
}

/////////////////////////////////////////////////////

type TestRpcServer struct{}

func (t TestRpcServer) GetName(ctx context.Context, req *GetNameRequest) (*GetNameResponse, error) {
	return &GetNameResponse{Name: req.Name}, nil
}

// ///////////////////////////////////////////////////
// 测试 JsonService 的 Invoke 方法
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
