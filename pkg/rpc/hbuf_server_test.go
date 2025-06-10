package rpc_test

import (
	"context"
	"github.com/wskfjtheqian/hbuf_golang/pkg/erro"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"github.com/wskfjtheqian/hbuf_golang/pkg/rpc"
	"github.com/wskfjtheqian/hbuf_golang/pkg/service"
)

type HbufService interface {
	Init(ctx context.Context)

	HbufMethod(ctx context.Context, req *HbufRequest) (*HbufResponse, error)
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
	response, err := rpc.ClientCall[*HbufRequest, *HbufResponse](ctx, r.client, 0, "hbuf_service", "hbuf_method", req)
	if err != nil {
		return nil, err
	}
	return response, nil
}

func RegisterHbufService(r rpc.ServerRegister, server HbufService) {
	r.Register(0, "hbuf_service",
		&rpc.MethodImpl[*HbufRequest, *HbufResponse]{
			Name: "hbuf_method",
			Handler: func(ctx context.Context, req hbuf.Data) (hbuf.Data, error) {
				return server.HbufMethod(ctx, req.(*HbufRequest))
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
