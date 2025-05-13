package rpc

import (
	"context"
	hbuf "github.com/wskfjtheqian/hbuf_golang/pkg/buf"
	"time"
)

type GetNameRequest struct {
	Name string `json:"name"`
}

func (r *GetNameRequest) Descriptors() hbuf.Descriptor {
	//TODO implement me
	panic("implement me")
}

func (r *GetNameRequest) Data() hbuf.Data {
	return r
}

type GetNameResponse struct {
	Name string `json:"name"`
}

func (r *GetNameResponse) Descriptors() hbuf.Descriptor {
	//TODO implement me
	panic("implement me")
}

func (r *GetNameResponse) Data() hbuf.Data {
	return r
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
		&MethodImpl[*GetNameRequest, *GetNameResponse]{
			Name: "GetName",
			Handler: func(ctx context.Context, req hbuf.Data) (hbuf.Data, error) {
				return server.GetName(ctx, req.(*GetNameRequest))
			},
		},
	)
}

/////////////////////////////////////////////////////

type TestRpcServer struct{}

func (t TestRpcServer) GetName(ctx context.Context, req *GetNameRequest) (*GetNameResponse, error) {
	<-time.After(time.Millisecond * 100)
	return &GetNameResponse{Name: req.Name}, nil
}
