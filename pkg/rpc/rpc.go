package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"io"
	"net/http"
	"reflect"
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

type Request func(ctx context.Context, path string, notification bool, callback func(writer io.Writer) error) (io.ReadCloser, error)

type RequestFilter func(ctx context.Context, request Request) Request

type Response func(ctx context.Context, path string, writer io.Writer, reader io.Reader) error

type ResponseFilter func(ctx context.Context, response Response) Response

// Handler 是用于处理RPC请求
type Handler func(ctx context.Context, req hbuf.Data) (hbuf.Data, error)

// HandlerFilter 是用于过滤Handler
type HandlerFilter func(ctx context.Context, handler Handler) Handler

////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

// WithContext 创建一个新的Context
func WithContext(ctx context.Context, method string) context.Context {
	return &Context{
		Context: ctx,
		header:  http.Header{},
		tags:    make(map[string][]any),
		method:  method,
	}
}

// Context 是用于处理RPC请求的上下文
type Context struct {
	context.Context
	header http.Header
	tags   map[string][]any
	method string
}

var contextType = reflect.TypeOf(&HttpContext{})

// Value 返回Context的value
func (d *Context) Value(key any) any {
	if reflect.TypeOf(d) == key {
		return d
	}
	return d.Context.Value(key)
}

// FromContext 从Context中获取Context
func FromContext(ctx context.Context) *Context {
	val := ctx.Value(contextType)
	if val == nil {
		return nil
	}
	return val.(*Context)
}

// AddHeader 添加Header
func AddHeader(ctx context.Context, key, value string) {
	d := FromContext(ctx)
	if d == nil {
		return
	}
	d.header.Set(key, value)
}

// GetHeader 获取Header
func GetHeader(ctx context.Context, key string) string {
	d := FromContext(ctx)
	if d == nil {
		return ""
	}
	return d.header.Get(key)
}

// GetHeaders 获取Headers
func GetHeaders(ctx context.Context) http.Header {
	d := FromContext(ctx)
	if d == nil {
		return nil
	}
	return d.header
}

// AddTag 添加Tag
func AddTag(ctx context.Context, key string, value any) {
	d := FromContext(ctx)
	if d == nil {
		return
	}
	d.tags[key] = append(d.tags[key], value)
}

// GetTag 获得Tag
func GetTag(ctx context.Context, key string) []any {
	d := FromContext(ctx)
	if d == nil {
		return nil
	}
	return d.tags[key]
}

// GetMethod 获取方法名
func (d *Context) GetMethod() string {
	return d.method
}

////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

type Method interface {
	GetId() uint32
	GetName() string
	GetHandler() Handler
	DecodeRequest(decoder func(v any) error) (hbuf.Data, error)
}

// MethodImpl 是用于处理RPC请求的接口
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
	panic("implement me")
}

func (r Result[T]) Decoder(reader io.Reader) (err error) {
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

// ////////////////////////////////////////////////////

// ServerOption 服务器选项
type ServerOption func(*Server)

// WithServerFilter 设置Handler过滤器
func WithServerFilter(filter HandlerFilter) ServerOption {
	return func(s *Server) {
		s.filter = filter
	}
}

func NewServer(options ...ServerOption) *Server {
	ret := &Server{
		methods: make(map[string]Method),
		decode:  NewJsonDecode(),
		encode:  NewJsonEncode(),
		filter: func(ctx context.Context, handler Handler) Handler {
			return handler
		},
	}
	for _, option := range options {
		option(ret)
	}
	return ret
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

	ctx = WithContext(ctx, method.GetName())
	response, err := r.filter(ctx, method.GetHandler())(ctx, request)
	if err != nil {
		return err
	}

	return r.encode(writer)(&Result[hbuf.Data]{
		Code: 0,
		Msg:  "ok",
		Data: response,
	})
}

//////////////////////////////////////////////////////

type ClientOption func(*Client)

// WithClientFilter 设置Handler过滤器
func WithClientFilter(filter HandlerFilter) ClientOption {
	return func(c *Client) {
		c.filter = filter
	}
}

// NewClient 创建一个新的客户端
func NewClient(request Request, options ...ClientOption) *Client {
	ret := &Client{
		request: request,
		decode:  NewJsonDecode(),
		encode:  NewJsonEncode(),
		filter: func(ctx context.Context, handler Handler) Handler {
			return handler
		},
	}

	for _, option := range options {
		option(ret)
	}
	return ret
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
		reader, err := c.request(ctx, name+method, false, func(writer io.Writer) error {
			return c.encode(writer)(req)
		})
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
