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

// Request 是用于处理RPC请求的函数
type Request func(ctx context.Context, path string, notification bool, callback func(writer io.Writer) error) (io.ReadCloser, error)

// RequestMiddleware 用于对 Request 进行中间件处理。
type RequestMiddleware func(next Request) Request

// Response 是用于处理RPC响应的函数
type Response func(ctx context.Context, path string, writer io.Writer, reader io.Reader) error

// ResponseMiddleware 用于对 Response 进行中间件处理。
type ResponseMiddleware func(next Response) Response

// Handler 是用于处理RPC请求
type Handler func(ctx context.Context, req hbuf.Data) (hbuf.Data, error)

// HandlerMiddleware 用于对 Handler 进行中间件处理。
type HandlerMiddleware func(next Handler) Handler

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

var contextType = reflect.TypeOf(&Context{})

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
	DecodeRequest(decoder func(v hbuf.Data) error) (hbuf.Data, error)
}

// MethodImpl 是用于处理RPC请求的接口
type MethodImpl[T any, E any] struct {
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

func (m *MethodImpl[T, E]) DecodeRequest(decoder func(v hbuf.Data) error) (hbuf.Data, error) {
	var request any = *new(T)
	err := decoder(request.(hbuf.Data))
	if err != nil {
		return nil, nil
	}

	return request.(hbuf.Data), nil
}

func (m *MethodImpl[T, E]) GetHandler() Handler {
	return m.Handler
}

//////////////////////////////////////////////////////

type Result[T any] struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data T      `json:"data"`
}

func (t *Result[T]) Encoder(w io.Writer) error {
	err := hbuf.WriterInt64(w, 1, int64(t.Code))
	if err != nil {
		return err
	}
	err = hbuf.WriterBytes(w, 2, []byte(t.Msg))
	if err != nil {
		return err
	}
	err = hbuf.WriterData(w, 3, t.Data)
	if err != nil {
		return err
	}
	return nil
}

func (t *Result[T]) Decoder(r io.Reader) error {
	return hbuf.Decoder(r, func(typ hbuf.Type, id uint16, value any) (err error) {
		switch id {
		case 1:
			t.Code, err = hbuf.ReaderNumber[int](value)
		case 2:
			t.Msg, err = hbuf.ReaderBytes[string](value)
		case 3:
			err = hbuf.ReaderData(value, t.Data.(hbuf.Data))
		}
		return nil
	})
}

func (t *Result[T]) Size() int {
	length := 0
	if t.Code != 0 {
		length += 1 + int(hbuf.LengthUint64(uint64(t.Code))) + int(hbuf.LengthUint64(1))
	}
	if t.Msg != "" {
		length += 1 + int(hbuf.LengthBytes([]byte(t.Msg))) + int(hbuf.LengthUint64(2))
	}
	if any(t.Data) != nil {
		temp := t.Data.Size()
		length += 1 + temp + int(hbuf.LengthUint64(3)) + int(hbuf.LengthUint64(uint64(temp)))
	}
	return length
}

type Encoder func(writer io.Writer) func(v hbuf.Data) error
type Decoder func(reader io.Reader) func(v hbuf.Data) error

// NewJsonEncode 编码器接口。
func NewJsonEncode() Encoder {
	return func(writer io.Writer) func(v hbuf.Data) error {
		return func(v hbuf.Data) error {
			return json.NewEncoder(writer).Encode(v)
		}
	}
}

// NewJsonDecode 解码器接口。
func NewJsonDecode() Decoder {
	return func(reader io.Reader) func(v hbuf.Data) error {
		return func(v hbuf.Data) error {
			return json.NewDecoder(reader).Decode(v)
		}
	}
}

// NewHBufEncode 编码器接口。
func NewHBufEncode() Encoder {
	return func(writer io.Writer) func(v hbuf.Data) error {
		return func(v hbuf.Data) error {
			return v.Encoder(writer)
		}
	}
}

// NewHBufDecode 解码器接口。
func NewHBufDecode() Decoder {
	return func(reader io.Reader) func(v hbuf.Data) error {
		return func(v hbuf.Data) error {
			return v.Decoder(reader)
		}
	}
}

// ////////////////////////////////////////////////////

// ServerOption 服务器选项
type ServerOption func(*Server)

// WithServerMiddleware 设置Handler中间件
func WithServerMiddleware(middleware ...HandlerMiddleware) ServerOption {
	return func(s *Server) {
		s.middleware = func(next Handler) Handler {
			for i := len(middleware) - 1; i >= 0; i-- {
				next = middleware[i](next)
			}
			return next
		}
	}
}

// WithServerDecode 设置解码器
func WithServerDecode(decoder Decoder) ServerOption {
	return func(s *Server) {
		s.decode = decoder
	}
}

// WithServerEncoder 设置编码器
func WithServerEncoder(encoder Encoder) ServerOption {
	return func(s *Server) {
		s.encode = encoder
	}
}

// NewServer 创建一个新的服务器
func NewServer(options ...ServerOption) *Server {
	ret := &Server{
		methods: make(map[string]Method),
		decode:  NewJsonDecode(),
		encode:  NewJsonEncode(),
		middleware: func(next Handler) Handler {
			return next
		},
	}
	for _, option := range options {
		option(ret)
	}
	return ret
}

// Server 是用于处理RPC请求的服务器
type Server struct {
	lock       sync.RWMutex
	methods    map[string]Method
	decode     Decoder
	encode     Encoder
	middleware HandlerMiddleware
}

// Register 注册方法
func (r *Server) Register(id int32, name string, methods ...Method) {
	name = strings.Trim(name, "/") + "/"
	r.lock.Lock()
	defer r.lock.Unlock()

	for _, method := range methods {
		key := strings.TrimLeft(method.GetName(), "/")
		r.methods[name+key] = method
	}
}

// Response 处理RPC请求
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
	response, err := r.middleware(method.GetHandler())(ctx, request)
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

// ClientOption 客户端选项
type ClientOption func(*Client)

// WithClientMiddleware 设置Handler中间件
func WithClientMiddleware(middleware ...HandlerMiddleware) ClientOption {
	return func(c *Client) {
		c.middleware = func(next Handler) Handler {
			for i := len(middleware) - 1; i >= 0; i-- {
				next = middleware[i](next)
			}
			return next
		}
	}
}

// WithClientDecode 设置解码器
func WithClientDecode(decoder Decoder) ClientOption {
	return func(c *Client) {
		c.decode = decoder
	}
}

// WithClientEncoder 设置编码器
func WithClientEncoder(encoder Encoder) ClientOption {
	return func(c *Client) {
		c.encode = encoder
	}
}

// NewClient 创建一个新的客户端
func NewClient(request Request, options ...ClientOption) *Client {
	ret := &Client{
		request: request,
		decode:  NewJsonDecode(),
		encode:  NewJsonEncode(),
		middleware: func(next Handler) Handler {
			return next
		},
	}

	for _, option := range options {
		option(ret)
	}
	return ret
}

// Client 是用于处理RPC请求的客户端
type Client struct {
	request    Request
	decode     Decoder
	encode     Encoder
	middleware HandlerMiddleware
}

// ClientCall 调用远程服务
func ClientCall[T any, E any](ctx context.Context, c *Client, id uint32, name string, method string, request *T) (E, error) {
	name = strings.Trim(name, "/") + "/"
	data, err := c.middleware(func(ctx context.Context, req hbuf.Data) (hbuf.Data, error) {
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

		var data any = result.Data
		return data.(hbuf.Data), nil
	})(ctx, *request)
	if err != nil {
		var v E
		return v, err
	}
	return data.(E), nil
}
