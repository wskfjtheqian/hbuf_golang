package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"io"
	"strings"
	"sync"
)

type Type int8

const (
	TypeRequest Type = iota
	TypeResponse
	TypeNotification
	TypeHeartbeat
	TypeAuthSuccess
	TypeAuthFailure
)

type Request func(ctx context.Context, path string, reader io.Reader) (io.ReadCloser, error)

type RequestFilter func(ctx context.Context, request Request) Request

type Response func(ctx context.Context, writer io.Writer, reader io.Reader) error

type ResponseFilter func(ctx context.Context, response Response) Response

// Handler 是用于处理RPC请求
type Handler func(ctx context.Context, req hbuf.Data) (hbuf.Data, error)

// HandlerFilter 是用于过滤Handler
type HandlerFilter func(ctx context.Context, handler Handler) Handler

//////////////////////////////////////////////////////

type Method interface {
	GetId() uint32
	GetName() string
	GetHandler() Handler
	DecodeRequest(decoder func(v any) error) (hbuf.Data, error)
}

// Method 是用于处理RPC请求的接口
type MethodImpl[T hbuf.Data, E hbuf.Data] struct {
	Id      uint32
	Name    string
	Handler Handler
}

func (m *MethodImpl[T, E]) GetId() uint32 {
	return m.Id
}

func (m *MethodImpl[T, E]) GetName() string {
	return m.Name
}

func (m *MethodImpl[T, E]) DecodeRequest(decoder func(v any) error) (hbuf.Data, error) {
	var request any = new(T)
	err := decoder(request)
	if err != nil {
		return nil, nil
	}

	return request.(hbuf.Data), nil
}

func (m *MethodImpl[T, E]) GetHandler() Handler {
	return m.Handler
}

//////////////////////////////////////////////////////

type Result[T hbuf.Data] struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data T      `json:"data"`
}

func (r Result[T]) Encoder(writer io.Writer) (err error) {
	//TODO implement me
	panic("implement me")
}

func (r Result[T]) Decoder(reader io.Reader) (err error) {
	//TODO implement me
	panic("implement me")
}

type Encoder func(writer io.Writer) func(v any) error
type Decoder func(reader io.Reader) func(v any) error

// NewJsonEncode 编码器接口。
func NewJsonEncode() Encoder {
	return func(writer io.Writer) func(v any) error {
		return json.NewEncoder(writer).Encode
	}
}

// NewJsonDecode 解码器接口。
func NewJsonDecode() Decoder {
	return func(reader io.Reader) func(v any) error {
		return json.NewDecoder(reader).Decode
	}
}

//////////////////////////////////////////////////////

func NewServer() *Server {
	return &Server{
		methods: make(map[string]Method),
		decode:  NewJsonDecode(),
		encode:  NewJsonEncode(),
		filter: func(ctx context.Context, handler Handler) Handler {
			return handler
		},
	}
}

type Server struct {
	lock    sync.RWMutex
	methods map[string]Method
	decode  Decoder
	encode  Encoder
	filter  HandlerFilter
}

func (r *Server) Register(id int32, name string, methods ...Method) {
	name = strings.Trim(name, "/") + "/"
	r.lock.Lock()
	defer r.lock.Unlock()

	for _, method := range methods {
		key := strings.TrimLeft(method.GetName(), "/")
		r.methods[name+key] = method
	}
}

func (r *Server) Response(ctx context.Context, path string, writer io.Writer, reader io.Reader) error {
	r.lock.RLock()
	method, ok := r.methods[path]
	r.lock.RUnlock()

	if !ok {
		return errors.New("method not found")
	}
	request, err := method.DecodeRequest(r.decode(reader))
	if err != nil {
		return err
	}

	response, err := r.filter(ctx, method.GetHandler())(ctx, request)
	if err != nil {
		return err
	}

	result := Result[hbuf.Data]{
		Code: 0,
		Msg:  "",
		Data: response,
	}
	err = r.encode(writer)(&result)
	if err != nil {
		return err
	}

	return nil
}

//////////////////////////////////////////////////////

// NewClient 创建一个新的客户端
func NewClient(request Request) *Client {
	return &Client{
		request: request,
		decode:  NewJsonDecode(),
		encode:  NewJsonEncode(),
		filter: func(ctx context.Context, handler Handler) Handler {
			return handler
		},
	}
}

// Client 是用于处理RPC请求的客户端
type Client struct {
	request Request
	decode  Decoder
	encode  Encoder
	filter  HandlerFilter
}

// ClientCall 调用远程服务
func ClientCall[T hbuf.Data, E hbuf.Data](ctx context.Context, c *Client, id uint32, name string, method string, request *T) (E, error) {
	name = strings.Trim(name, "/") + "/"
	data, err := c.filter(ctx, func(ctx context.Context, req hbuf.Data) (hbuf.Data, error) {
		writer := bytes.NewBuffer(nil)
		err := c.encode(writer)(req)
		if err != nil {
			return nil, err
		}

		reader, err := c.request(ctx, name+method, writer)
		if err != nil {
			return nil, err
		}
		defer reader.Close()

		var result Result[E]
		err = c.decode(reader)(&result)
		if err != nil {
			return nil, err
		}

		if result.Code != 0 {
			return nil, errors.New(result.Msg)
		}

		return result.Data, nil
	})(ctx, *request)
	if err != nil {
		var v E
		return v, err
	}
	return data.(E), nil
}
