package hrpc

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
)

type Type int8

const (
	TypeRequest Type = iota
	TypeResponse
	TypeNotification
	TypePing
	TypePong
)

// CodeNotFound 方法未找到错误码,对应 HTTP 404
const CodeNotFound int32 = -1

// Request 是用于处理RPC请求的函数
type Request func(ctx context.Context, path string, notification bool, callback func(writer io.Writer) error) (io.ReadCloser, error)

// RequestMiddleware 用于对 Request 进行中间件处理。
type RequestMiddleware func(next Request) Request

// Response 是用于处理RPC响应的函数
type Response func(ctx context.Context, path string, writer io.Writer, reader io.Reader, header http.Header) error

// ResponseMiddleware 用于对 Response 进行中间件处理。
type ResponseMiddleware func(next Response) Response

// Handler 是用于处理RPC请求
type Handler func(ctx context.Context, req any) (any, error)

// Middleware 用于对 Handler 进行中间件处理。
type Middleware func(next Handler) Handler

////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

// WithContext 创建一个新的Context
func WithContext(ctx context.Context, method string, header http.Header) context.Context {
	return &Context{
		Context: ctx,
		header:  header,
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

func (r *Result[T]) Descriptors() hbuf.Descriptor {
	return r.descriptor
}

// resultDescriptorCache 缓存每种 T 的 Result descriptor,
// 避免每次请求动态构建(map + unsafe.Offsetof + sort),实测每次构建约 800ns。
var resultDescriptorCache sync.Map // key: reflect.Type, value: hbuf.Descriptor

// resultDescriptor 返回 *Result[T] 的 Descriptor 单例
func resultDescriptor[T hbuf.Data]() hbuf.Descriptor {
	typ := reflect.TypeOf((*Result[T])(nil))
	if d, ok := resultDescriptorCache.Load(typ); ok {
		return d.(hbuf.Descriptor)
	}
	d := buildResultDescriptor[T]()
	actual, _ := resultDescriptorCache.LoadOrStore(typ, d)
	return actual.(hbuf.Descriptor)
}

// buildResultDescriptor 构建 *Result[T] 的 Descriptor。
// T 为接口类型(服务端 NewResult(0, "ok", val) 推断为 hbuf.Data)时,
// Data 字段存 itab+数据指针,内层 descriptor 由运行时值决定,
// 使用 interfaceDataField 动态绑定;T 为具体指针类型时直接静态绑定。
func buildResultDescriptor[T hbuf.Data]() hbuf.Descriptor {
	var zero T
	typ := reflect.TypeOf(&zero).Elem()
	fieldMap := map[uint16]hbuf.Descriptor{
		1: hbuf.NewInt32Descriptor(unsafe.Offsetof(Result[T]{}.Code), false),
		2: hbuf.NewStringDescriptor(unsafe.Offsetof(Result[T]{}.Msg), false),
	}
	if typ.Kind() == reflect.Interface {
		fieldMap[3] = &interfaceDataField{offset: unsafe.Offsetof(Result[T]{}.Data)}
	} else {
		fieldMap[3] = hbuf.NewRefDescriptor(
			reflect.New(typ.Elem()).Interface().(hbuf.Data),
			unsafe.Offsetof(Result[T]{}.Data),
			true,
		)
	}
	return hbuf.NewDataDescriptor(0, false, reflect.TypeOf((*Result[T])(nil)), nil, fieldMap)
}

// interfaceDataField 是 Result[hbuf.Data] 中 Data 接口字段的动态绑定 Descriptor。
// 接口字段布局为 itab(1 word)+ 数据指针(1 word),内层 descriptor 由运行时值决定,
// 无法静态缓存,故在 Encode 时从接口字段动态获取。
// 解码方向无此用法(客户端统一用 NewResultResponse[T] 具体类型),返回错误。
type interfaceDataField struct {
	offset uintptr // Data 字段在 Result 中的偏移
}

func (d *interfaceDataField) IsEmpty(p unsafe.Pointer) bool {
	if p == nil {
		return true
	}
	// 接口第二字(数据指针)为 nil 即空
	return *(*unsafe.Pointer)(unsafe.Add(p, d.offset+unsafe.Sizeof(unsafe.Pointer(nil)))) == nil
}

// GetValue 返回接口字段本身的地址,由 Encode 重新读取接口值
func (d *interfaceDataField) GetValue(p unsafe.Pointer) unsafe.Pointer {
	if p == nil {
		return nil
	}
	return unsafe.Add(p, d.offset)
}

func (d *interfaceDataField) SetValue(p unsafe.Pointer) unsafe.Pointer {
	if p == nil {
		return nil
	}
	return unsafe.Add(p, d.offset)
}

func (d *interfaceDataField) Encode(buf []byte, p unsafe.Pointer, id *uint16) []byte {
	if p == nil {
		return buf
	}
	v := *(*hbuf.Data)(p) // 接口值
	if v == nil {
		return buf
	}
	ptr := *(*unsafe.Pointer)(unsafe.Add(p, unsafe.Sizeof(unsafe.Pointer(nil)))) // 数据指针
	if ptr == nil {
		return buf
	}
	return v.Descriptors().Encode(buf, ptr, id)
}

func (d *interfaceDataField) Decode(buf []byte, p unsafe.Pointer, typ hbuf.Type, valRead bool, valueLen uint8) ([]byte, error) {
	return buf, errors.New("hrpc: decoding into Result[hbuf.Data] interface field is unsupported, use NewResultResponse[T] with a concrete type")
}

func NewResult[T hbuf.Data](code int32, msg string, data T) *Result[T] {
	return &Result[T]{
		Code:       code,
		Msg:        msg,
		Data:       data,
		descriptor: resultDescriptor[T](),
	}
}

func NewResultResponse[T hbuf.Data]() *Result[T] {
	return &Result[T]{
		descriptor: resultDescriptor[T](),
	}
}

type Encoder func(writer io.Writer) func(v hbuf.Data) error
type Decoder func(reader io.Reader) func(v hbuf.Data) error

var marshalers = json.WithMarshalers(
	json.JoinMarshalers(
		json.MarshalToFunc(func(encoder *jsontext.Encoder, t time.Time) error {
			return encoder.WriteToken(jsontext.Int(t.UnixMilli()))
		}),
		json.MarshalToFunc(func(enc *jsontext.Encoder, v int64) error {
			return enc.WriteToken(jsontext.String(strconv.FormatInt(v, 10)))
		}),
		json.MarshalToFunc(func(enc *jsontext.Encoder, v uint64) error {
			return enc.WriteToken(jsontext.String(strconv.FormatUint(v, 10)))
		}),
	),
)

func GetMarshalers() json.Options {
	return marshalers
}

// NewJsonEncode 编码器接口。
func NewJsonEncode() Encoder {
	return func(writer io.Writer) func(v hbuf.Data) error {
		return func(v hbuf.Data) error {
			buffer, err := json.Marshal(v, marshalers)
			if err != nil {
				return herror.Wrap(err)
			}
			_, err = writer.Write(buffer)
			return err
		}
	}
}

var unmarshalers = json.WithUnmarshalers(
	json.JoinUnmarshalers(
		json.UnmarshalFromFunc(func(dec *jsontext.Decoder, t *time.Time) error {
			// 用 PeekKind 预判 null，不消费 token
			if dec.PeekKind() == 'n' {
				return errors.ErrUnsupported // 替代 SkipFunc
			}

			tok, err := dec.ReadToken()
			if err != nil {
				return err
			}
			if tok.Kind() != '0' { // '0' 表示数字
				return errors.New("time: expected number")
			}
			ms, err := strconv.ParseInt(tok.String(), 10, 64)
			if err != nil {
				return err
			}
			*t = time.UnixMilli(ms)
			return nil
		}),
		json.UnmarshalFromFunc(func(dec *jsontext.Decoder, v *int64) error {
			tok, err := dec.ReadToken()
			if err != nil {
				return err
			}
			if tok.Kind() != '"' {
				return errors.ErrUnsupported
			}
			*v, err = strconv.ParseInt(tok.String(), 10, 64)
			return err
		}),
		json.UnmarshalFromFunc(func(dec *jsontext.Decoder, v *uint64) error {
			tok, err := dec.ReadToken()
			if err != nil {
				return err
			}
			if tok.Kind() != '"' {
				return errors.ErrUnsupported
			}
			*v, err = strconv.ParseUint(tok.String(), 10, 64)
			return err
		}),
	),
)

func GetUnmarshalers() json.Options {
	return unmarshalers
}

// NewJsonDecode 解码器接口。
func NewJsonDecode() Decoder {
	return func(reader io.Reader) func(v hbuf.Data) error {
		return func(v hbuf.Data) error {
			buffer, err := io.ReadAll(reader)
			if err != nil {
				return err
			}
			return json.Unmarshal(buffer, v, unmarshalers)
		}
	}
}

// NewHBufEncode 编码器接口。
func NewHBufEncode() Encoder {
	return func(writer io.Writer) func(v hbuf.Data) error {
		return func(v hbuf.Data) error {
			buffer, err := hbuf.Marshal(v)
			if err != nil {
				return herror.Wrap(err)
			}
			_, err = writer.Write(buffer)
			return err
		}
	}
}

// NewHBufDecode 解码器接口。
func NewHBufDecode() Decoder {
	return func(reader io.Reader) func(v hbuf.Data) error {
		return func(v hbuf.Data) error {
			buffer, err := io.ReadAll(reader)
			if err != nil {
				return err
			}
			return hbuf.Unmarshal(buffer, v)
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
func WithServerMiddleware(middleware ...Middleware) ServerOption {
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
	middleware Middleware
	wait       sync.WaitGroup
	shutdowns  []func(ctx context.Context) error
}

// Register 注册方法。id 为服务分组标识,当前版本未使用,保留用于后续按 id 分组路由。
func (s *Server) Register(id int32, name string, init Init, methods ...*Method) {
	name = strings.Trim(name, "/") + "/"
	s.lock.Lock()
	defer s.lock.Unlock()

	s.inits[name] = init

	for _, method := range methods {
		key := strings.TrimLeft(method.Name, "/")
		s.methods[name+key] = method
	}
}

// Response 处理RPC请求
func (s *Server) Response(ctx context.Context, path string, writer io.Writer, reader io.Reader, header http.Header) error {
	s.wait.Add(1)
	defer s.wait.Done()

	s.lock.RLock()
	method, ok := s.methods[path]
	s.lock.RUnlock()

	if !ok {
		return NewResult[hbuf.Data](CodeNotFound, "method not found", nil)
	}

	var err error
	var in any
	if method.Decode == nil {
		in = reader
	} else {
		in, err = method.Decode(func(v hbuf.Data) (hbuf.Data, error) {
			err := s.decode(reader)(v)
			if err != nil {
				return nil, err
			}
			return v, nil
		})
		if err != nil {
			return err
		}
	}

	ctx = WithContext(ctx, method.Name, header)
	ctx = method.WithContext(ctx)

	traceId := hlog.FromContext(ctx)
	if len(traceId) == 0 {
		traceId = GetHeader(ctx, "X-Trace-Id")
		if len(traceId) > 0 {
			ctx = hlog.WithContext(ctx, traceId)
		}
	}
	response, err := s.middleware(method.Handler)(ctx, in)
	if val, ok := response.(io.Reader); ok {
		_, err = io.Copy(writer, val)
		return err
	} else if err != nil {
		if val, ok := err.(ResultI); ok && val.GetCode() != CodeNotFound {
			err = s.encode(writer)(val)
		}
		return err
	}
	if val, ok := response.(hbuf.Data); ok {
		return s.encode(writer)(NewResult(0, "ok", val))
	}
	return herror.NewError("handler response is not io.Reader or hbuf.Data")
}

func (s *Server) Init(ctx context.Context, list ...string) error {
	ctx, cancelFunc := context.WithCancel(ctx)
	defer cancelFunc()

	_, err := s.middleware(func(ctx context.Context, req any) (any, error) {
		if len(list) > 0 {
			for _, key := range list {
				if init, ok := s.inits[key+"/"]; ok {
					init.Init(ctx)
					hlog.Info(ctx, "finish init %s", strings.TrimRight(key, "/"))
				}
			}
		} else {
			for key, init := range s.inits {
				init.Init(ctx)
				hlog.Info(ctx, "finish init %s", strings.TrimRight(key, "/"))
			}
		}
		return nil, nil
	})(WithContext(ctx, "init", http.Header{}), nil)

	return err
}

// StatusFromError 将 RPC 错误映射为 HTTP 状态码。
// 返回的第二个值表示是否属于服务端内部错误(需要记录日志)。
func StatusFromError(err error) (int, bool) {
	var e *Result[hbuf.Data]
	if errors.As(err, &e) && e.Code == CodeNotFound {
		return http.StatusNotFound, false
	}
	return http.StatusInternalServerError, true
}

// OnShutdown 注册关闭钩子,Shutdown 时按注册顺序执行
func (s *Server) OnShutdown(hook func(ctx context.Context) error) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.shutdowns = append(s.shutdowns, hook)
}

func (s *Server) Shutdown(ctx context.Context) error {
	s.lock.RLock()
	hooks := append([]func(ctx context.Context) error{}, s.shutdowns...)
	s.lock.RUnlock()
	for _, hook := range hooks {
		if err := hook(ctx); err != nil {
			return err
		}
	}
	s.wait.Wait()
	return nil
}

//////////////////////////////////////////////////////

// ClientOption 客户端选项
type ClientOption func(*Client)

// WithClientMiddleware 设置Handler中间件
func WithClientMiddleware(middleware ...Middleware) ClientOption {
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
	middleware Middleware
}

// Invoke 调用远程方法。id 为服务分组标识,当前版本未使用,保留用于后续按 id 分组路由。
func (c *Client) Invoke(ctx context.Context, id uint32, name string, method string, request any, response any) (any, error) {
	name = strings.Trim(name, "/") + "/"
	data, err := c.middleware(func(ctx context.Context, req any) (any, error) {
		traceId := hlog.FromContext(ctx)
		if len(traceId) > 0 {
			AddHeader(ctx, "X-Trace-Id", traceId)
		}
		reader, err := c.request(ctx, name+method, response == nil, func(writer io.Writer) error {
			if val, ok := request.(io.Reader); ok {
				_, err := io.Copy(writer, val)
				return err
			} else if val, ok := request.(hbuf.Data); ok {
				return c.encode(writer)(val)
			} else {
				return herror.NewError("request is not io.Reader or hbuf.Data")
			}
		})
		if err != nil {
			return nil, err
		}
		if val, ok := response.(ResultI); ok {
			defer reader.Close()
			err = c.decode(reader)(val)
			if err != nil {
				return nil, err
			}

			if val.GetCode() != 0 {
				return nil, val
			}

			return val.GetData(), nil
		} else {
			return reader, nil
		}
	})(WithContext(ctx, name+method, http.Header{}), request)
	if err != nil {
		return nil, err
	}
	return data, nil
}
