package rpc

import (
	"context"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"io"
	"time"
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
	<-time.After(time.Millisecond * 100)
	return &GetNameResponse{Name: req.Name}, nil
}
