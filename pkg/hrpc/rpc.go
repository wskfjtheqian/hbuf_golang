package hrpc

import (
	"context"
	"errors"
	hbuf "github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hjson"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"unsafe"
)

type Type int8

const (
	TypeRequest Type = iota
	TypeResponse
	TypeNotification
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
type Handler func(ctx context.Context, req any) (any, error)

// HandlerMiddleware 用于对 Handler 进行中间件处理。
type HandlerMiddleware func(next Handler) Handler

////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

// WithContext 创建一个新的Context
func WithContext(ctx context.Context, method string) context.Context {
	return &Context{
		Context: ctx,
		header:  http.Header{},
		tags:    make(map[string][]string),
		method:  method,
	}
}

// Context 是用于处理RPC请求的上下文
type Context struct {
	context.Context
	header http.Header
	tags   map[string][]string
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
func AddTag(ctx context.Context, key string, value ...string) {
	d := FromContext(ctx)
	if d == nil {
		return
	}
	d.tags[key] = append(d.tags[key], value...)
}

// GetTag 获得Tag
func GetTag(ctx context.Context, key string) []string {
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

type Method struct {
	Id          uint32
	Name        string
	Handler     Handler
	WithContext func(ctx context.Context) context.Context
	Decode      func(decoder func(v hbuf.Data) (hbuf.Data, error)) (hbuf.Data, error)
	Tag         string
}

// ////////////////////////////////////////////////////
type ResultI interface {
	GetCode() int32

	Error() string

	GetData() any

	Descriptors() hbuf.Descriptor
}

type Result[T hbuf.Data] struct {
	Code       int32  `json:"code"`
	Msg        string `json:"msg"`
	Data       T      `json:"data"`
	descriptor hbuf.Descriptor
}

func (r *Result[T]) GetCode() int32 {
	return r.Code
}

func (r *Result[T]) Error() string {
	return r.Msg
}

func (r *Result[T]) GetData() any {
	return r.Data
}

func NewResult[T hbuf.Data](code int32, msg string, data T) *Result[T] {
	ret := &Result[T]{
		Code: code,
		Msg:  msg,
		Data: data,
	}
	descriptor := map[uint16]hbuf.Descriptor{
		1: hbuf.NewInt32Descriptor(unsafe.Offsetof(ret.Code), false),
		2: hbuf.NewStringDescriptor(unsafe.Offsetof(ret.Msg), false),
	}
	if any(data) != nil {
		descriptor[3] = hbuf.CloneDataDescriptor(data, unsafe.Offsetof(ret.Data)+unsafe.Sizeof(&ret.Data), true)
	}
	ret.descriptor = hbuf.NewDataDescriptor(0, false, reflect.TypeOf(ret), nil, descriptor)
	return ret
}

func NewResultResponse[T hbuf.Data]() *Result[T] {
	ret := &Result[T]{}
	descriptor := map[uint16]hbuf.Descriptor{
		1: hbuf.NewInt32Descriptor(unsafe.Offsetof(ret.Code), false),
		2: hbuf.NewStringDescriptor(unsafe.Offsetof(ret.Msg), false),
		3: hbuf.CloneDataDescriptor(reflect.New(reflect.TypeOf(ret.Data).Elem()).Interface().(hbuf.Data), unsafe.Offsetof(ret.Data), true),
	}
	ret.descriptor = hbuf.NewDataDescriptor(0, false, reflect.TypeOf(ret), nil, descriptor)
	return ret
}

func (r *Result[T]) Descriptors() hbuf.Descriptor {
	return r.descriptor
}

type Encoder func(writer io.Writer) func(v hbuf.Data, tag string) error
type Decoder func(reader io.Reader) func(v hbuf.Data, tag string) error

// NewJsonEncode 编码器接口。
func NewJsonEncode() Encoder {
	return func(writer io.Writer) func(v hbuf.Data, tag string) error {
		return func(v hbuf.Data, tag string) error {
			buffer, err := hjson.Marshal(v, tag)
			if err != nil {
				return err
			}
			_, err = writer.Write(buffer)
			if err != nil {
				return err
			}
			return nil
		}
	}
}

// NewJsonDecode 解码器接口。
func NewJsonDecode() Decoder {
	return func(reader io.Reader) func(v hbuf.Data, tag string) error {
		return func(v hbuf.Data, tag string) error {
			buffer, err := io.ReadAll(reader)
			if err != nil {
				return err
			}
			err = hjson.Unmarshal(buffer, v, "")
			if err != nil {
				return err
			}
			return nil
		}
	}
}

// NewHBufEncode 编码器接口。
func NewHBufEncode() Encoder {
	return func(writer io.Writer) func(v hbuf.Data, tag string) error {
		return func(v hbuf.Data, tag string) error {
			buffer, err := hbuf.Marshal(v, "")
			if err != nil {
				return err
			}
			_, err = writer.Write(buffer)
			if err != nil {
				return err
			}
			return nil
		}
	}
}

// NewHBufDecode 解码器接口。
func NewHBufDecode() Decoder {
	return func(reader io.Reader) func(v hbuf.Data, tag string) error {
		return func(v hbuf.Data, tag string) error {
			buffer, err := io.ReadAll(reader)
			if err != nil {
				return err
			}
			err = hbuf.Unmarshal(buffer, v, "")
			if err != nil {
				return err
			}
			return nil
		}
	}
}

// ////////////////////////////////////////////////////
type Init interface {
	Init(ctx context.Context)
}

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
		inits:   make(map[string]Init),
		methods: make(map[string]*Method),
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

type ServerRegister interface {
	Register(id int32, name string, init Init, methods ...*Method)
}

// Server 是用于处理RPC请求的服务器
type Server struct {
	lock       sync.RWMutex
	inits      map[string]Init
	methods    map[string]*Method
	decode     Decoder
	encode     Encoder
	middleware HandlerMiddleware
}

// Register 注册方法
func (r *Server) Register(id int32, name string, init Init, methods ...*Method) {
	name = strings.Trim(name, "/") + "/"
	r.lock.Lock()
	defer r.lock.Unlock()

	r.inits[name] = init

	for _, method := range methods {
		key := strings.TrimLeft(method.Name, "/")
		r.methods[name+key] = method
	}
}

// Response 处理RPC请求
func (r *Server) Response(ctx context.Context, path string, writer io.Writer, reader io.Reader) error {
	r.lock.RLock()
	method, ok := r.methods[path]
	r.lock.RUnlock()

	if !ok {
		return NewResult[hbuf.Data](-1, "method not found", nil)
	}

	var err error
	var in any
	if method.Decode == nil {
		in = reader
	} else {
		in, err = method.Decode(func(v hbuf.Data) (hbuf.Data, error) {
			err := r.decode(reader)(v, method.Tag)
			if err != nil {
				return nil, err
			}
			return v, nil
		})
		if err != nil {
			return err
		}
	}

	ctx = WithContext(ctx, method.Name)
	ctx = method.WithContext(ctx)
	response, err := r.middleware(method.Handler)(ctx, in)
	if val, ok := response.(io.Reader); ok {
		_, err = io.Copy(writer, val)
		return err
	} else if err != nil {
		var e *Result[hbuf.Data]
		if errors.As(err, &e) && e.Code != -1 {
			err = r.encode(writer)(e, method.Tag)
		}
		return err
	}
	return r.encode(writer)(NewResult(0, "ok", response.(hbuf.Data)), "")
}

func (r *Server) Init() {
	ctx, cancelFunc := context.WithCancel(context.TODO())
	defer cancelFunc()
	_, _ = r.middleware(func(ctx context.Context, req any) (any, error) {
		for _, init := range r.inits {
			init.Init(ctx)
		}
		return nil, nil
	})(ctx, nil)
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

func (c *Client) Invoke(ctx context.Context, id uint32, name string, method string, tag string, request any, response any) (any, error) {
	name = strings.Trim(name, "/") + "/"
	data, err := c.middleware(func(ctx context.Context, req any) (any, error) {
		reader, err := c.request(ctx, name+method, false, func(writer io.Writer) error {
			if val, ok := request.(io.Reader); ok {
				_, err := io.Copy(writer, val)
				return err
			} else if val, ok := request.(hbuf.Data); ok {
				return c.encode(writer)(val, tag)
			} else {
				return herror.NewError("request is not io.Reader or hbuf.Data")
			}
		})
		if err != nil {
			return nil, err
		}
		if val, ok := response.(ResultI); ok {
			defer reader.Close()

			err = c.decode(reader)(val, tag)
			if err != nil {
				return nil, err
			}

			if val.GetCode() != 0 {
				return nil, errors.New(val.Error())
			}

			return val.GetData(), nil
		} else {
			return reader, nil
		}
	})(ctx, request)
	if err != nil {
		return nil, err
	}
	return data, nil
}
