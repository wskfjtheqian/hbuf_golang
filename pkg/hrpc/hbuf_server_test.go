package hrpc_test

import (
	"context"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hrpc"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hservice"
	"io"
)

type HbufService interface {
	Init(ctx context.Context)

	HbufMethod(ctx context.Context, req *HbufRequest) (*HbufResponse, error)

	HbufStream(ctx context.Context, reader io.Reader) (io.ReadCloser, error)
}

type HbufServiceClient struct {
	client *hrpc.Client
}

func (p *HbufServiceClient) Init(ctx context.Context) {
}

func NewHbufServiceClient(client *hrpc.Client) HbufService {
	return &HbufServiceClient{
		client: client,
	}
}

func (r *HbufServiceClient) HbufMethod(ctx context.Context, req *HbufRequest) (*HbufResponse, error) {
	response, err := r.client.Invoke(ctx, 0, "hbuf_service", "hbuf_method", "", req, hrpc.NewResultResponse[*HbufResponse]())
	if err != nil {
		return nil, err
	}
	return response.(*HbufResponse), nil
}

func (r *HbufServiceClient) HbufStream(ctx context.Context, req io.Reader) (io.ReadCloser, error) {
	response, err := r.client.Invoke(ctx, 0, "hbuf_service", "hbuf_stream", "", req, nil)
	if err != nil {
		return nil, err
	}
	return response.(io.ReadCloser), nil
}

func RegisterHbufService(r hrpc.ServerRegister, server HbufService) {
	r.Register(0, "hbuf_service", server,
		&hrpc.Method{
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
		&hrpc.Method{
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
	return nil, herror.NewError("not find server hbuf_service")
}

func (s *DefaultHbufService) HbufStream(ctx context.Context, reader io.Reader) (io.ReadCloser, error) {
	return nil, herror.NewError("not find server hbuf_service")
}

var NotFoundHbufService = &DefaultHbufService{}

func GetHbufService(ctx context.Context) HbufService {
	router := hservice.GetClient(ctx, "hbuf_service")
	if nil == router {
		return NotFoundHbufService
	}
	if val, ok := router.(HbufService); ok {
		return val
	}
	return NotFoundHbufService
}
