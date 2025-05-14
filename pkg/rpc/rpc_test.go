package rpc

import (
	"context"
	hbuf "github.com/wskfjtheqian/hbuf_golang/pkg/buf"
	"reflect"
	"time"
	"unsafe"
)

var getNameRequest GetNameRequest
var getNameRequestDescriptor = hbuf.NewDataDescriptor(0, false, reflect.TypeOf(GetNameRequest{}), map[uint16]hbuf.Descriptor{
	1: hbuf.NewStringDescriptor(unsafe.Offsetof(getNameRequest.Name), false),
})

type GetNameRequest struct {
	Name string `json:"name"`
}

func (r *GetNameRequest) Descriptors() hbuf.Descriptor {
	return getNameRequestDescriptor
}

var getNameResponse GetNameResponse
var getNameResponseDescriptor = hbuf.NewDataDescriptor(0, false, reflect.TypeOf(GetNameResponse{}), map[uint16]hbuf.Descriptor{
	1: hbuf.NewStringDescriptor(unsafe.Offsetof(getNameResponse.Name), false),
})

type GetNameResponse struct {
	Name string `json:"name"`
}

func (r *GetNameResponse) Descriptors() hbuf.Descriptor {
	return getNameResponseDescriptor
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

func (t *TestRpcClient) GetName(ctx context.Context, req *GetNameRequest) (*GetNameResponse, error) {
	response, err := ClientCall[*GetNameRequest, *GetNameResponse](ctx, t.client, 0, "TestRpc", "GetName", req)
	if err != nil {
		return nil, err
	}
	return response, nil
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
