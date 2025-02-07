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

func (g *GetNameRequest) Encoder(w io.Writer) (err error) {
	err = hbuf.WriterBytes(w, 1, []byte(g.Name))
	if err != nil {
		return
	}
	return nil
}

func (g *GetNameRequest) Decoder(r io.Reader) (err error) {
	return hbuf.Decoder(r, func(typ hbuf.Type, id uint16, value any) error {
		switch id {
		case 1:
			g.Name, err = hbuf.ReaderBytes[string](value)
		}
		return nil
	})
}

func (g *GetNameRequest) Size() int {
	length := 0
	if g.Name != "" {
		length += 1 + int(hbuf.LengthBytes([]byte(g.Name))) + int(hbuf.LengthUint64(1))
	}
	return length
}

type GetNameResponse struct {
	Name string `json:"name"`
}

func (r *GetNameResponse) Data() hbuf.Data {
	return hbuf.Data(r)
}
func (g *GetNameResponse) Encoder(w io.Writer) (err error) {
	err = hbuf.WriterBytes(w, 1, []byte(g.Name))
	if err != nil {
		return
	}
	return nil
}

func (g *GetNameResponse) Decoder(r io.Reader) (err error) {
	return hbuf.Decoder(r, func(typ hbuf.Type, id uint16, value any) error {
		switch id {
		case 1:
			g.Name, err = hbuf.ReaderBytes[string](value)
		}
		return nil
	})
}

func (g *GetNameResponse) Size() int {
	length := 0
	if g.Name != "" {
		length += 1 + int(hbuf.LengthBytes([]byte(g.Name))) + int(hbuf.LengthUint64(1))
	}
	return length
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
				temp := req.(GetNameRequest)
				return server.GetName(ctx, &temp)
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
