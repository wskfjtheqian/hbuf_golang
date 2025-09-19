package rpc_test

import (
	"context"
	"github.com/wskfjtheqian/hbuf_golang/pkg/erro"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"github.com/wskfjtheqian/hbuf_golang/pkg/rpc"
	"github.com/wskfjtheqian/hbuf_golang/pkg/service"
	"io"
)

type HbufService interface {
	Init(ctx context.Context)

	HbufMethod(ctx context.Context, req *HbufRequest) (*HbufResponse, error)

	HbufStream(ctx context.Context, reader io.Reader) (io.ReadCloser, error)
}

type HbufServiceClient struct {
	client *rpc.Client
}

func (p *HbufServiceClient) Init(ctx context.Context) {
}

func NewHbufServiceClient(client *rpc.Client) HbufService {
	return &HbufServiceClient{
		client: client,
	}
}

func (r *HbufServiceClient) HbufMethod(ctx context.Context, req *HbufRequest) (*HbufResponse, error) {
	response, err := r.client.Invoke(ctx, 0, "hbuf_service", "hbuf_method", req, rpc.NewResultResponse[*HbufResponse]())
	if err != nil {
		return nil, err
	}
	return response.(*HbufResponse), nil
}

func (r *HbufServiceClient) HbufStream(ctx context.Context, req io.Reader) (io.ReadCloser, error) {
	response, err := r.client.Invoke(ctx, 0, "hbuf_service", "hbuf_stream", req, nil)
	if err != nil {
		return nil, err
	}
	return response.(io.ReadCloser), nil
}

func RegisterHbufService(r rpc.ServerRegister, server HbufService) {
	r.Register(0, "hbuf_service",
		&rpc.Method{
			Name: "hbuf_method",
			WithContext: func(ctx context.Context) context.Context {
				return ctx
			},
			Handler: func(ctx context.Context, req any) (any, error) {
				return server.HbufMethod(ctx, req.(*HbufRequest))
			},
			Decode: func(decoder func(v hbuf.Data) (hbuf.Data, error)) (hbuf.Data, error) {
				return decoder(&HbufRequest{})
			},
		},
		&rpc.Method{
			Name: "hbuf_stream",
			WithContext: func(ctx context.Context) context.Context {
				return ctx
			},
			Handler: func(ctx context.Context, req any) (any, error) {
				return server.HbufStream(ctx, req.(io.Reader))
			},
		},
	)
}

type DefaultHbufService struct {
}

func (s *DefaultHbufService) Init(ctx context.Context) {
}

func (s *DefaultHbufService) HbufMethod(ctx context.Context, req *HbufRequest) (*HbufResponse, error) {
	return nil, erro.NewError("not find server hbuf_service")
}

func (s *DefaultHbufService) HbufStream(ctx context.Context, reader io.Reader) (io.ReadCloser, error) {
	return nil, erro.NewError("not find server hbuf_service")
}

var NotFoundHbufService = &DefaultHbufService{}

func GetHbufService(ctx context.Context) HbufService {
	router := service.GetClient(ctx, "hbuf_service")
	if nil == router {
		return NotFoundHbufService
	}
	if val, ok := router.(HbufService); ok {
		return val
	}
	return NotFoundHbufService
}
