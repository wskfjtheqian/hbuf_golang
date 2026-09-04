package hhttp

import (
	"bufio"
	"context"
	"errors"
	"net"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hip"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hutl"
)

type Option func(*Http)

func WithNewContext(newContext func() context.Context) Option {
	return func(s *Http) {
		s.newContext = newContext
	}
}

func WithListenConfig(lc net.ListenConfig) Option {
	return func(s *Http) {
		s.lc = lc
	}
}

type Http struct {
	mux         http.ServeMux
	http        atomic.Pointer[http.Server]
	config      atomic.Pointer[Config]
	lc          net.ListenConfig
	init        chan bool
	isInit      atomic.Pointer[bool]
	log         *hlog.Logger
	builderPool sync.Pool
	newContext  func() context.Context
}

func NewHttp(options ...Option) *Http {
	hlog.SetLevelName(LogHTTP, "HTTP")

	ret := &Http{
		init: make(chan bool, 1),
		log:  hlog.NewLogger("", hlog.LstdFlags),
		builderPool: sync.Pool{
			New: func() any {
				return &strings.Builder{}
			},
		},
		lc: net.ListenConfig{
			Control: controlSocket,
		},
	}
	ret.isInit.Store(hutl.ToPointer(false))
	for _, option := range options {
		option(ret)
	}

	return ret
}

func (a *Http) IsOpen() bool {
	return a.config.Load() != nil
}

func (a *Http) Init(ctx context.Context) {
	a.isInit.Store(hutl.ToPointer(true))
	a.init <- true
}
func (a *Http) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	a.mux.HandleFunc(pattern, handler)
}

func (a *Http) Handle(pattern string, handler http.Handler) {
	a.mux.Handle(pattern, handler)
}

// SetConfig 设置配置
func (a *Http) SetConfig(ctx context.Context, conf *Config) error {
	if a.config.Load().Equal(conf) {
		return nil
	}
	if nil == conf {
		h := a.http.Swap(nil)
		if h != nil {
			_ = h.Close()
			hlog.Info(ctx, "close old http connection")
		}
		return nil
	}

	listener, err := a.lc.Listen(ctx, "tcp", *conf.Addr)
	if err != nil {
		hlog.Error(ctx, "Listen server failed with '%s'\n", err)
		return nil
	}

	go func() {
		if !*a.isInit.Load() {
			<-a.init
		}

		h := &http.Server{
			Handler: a,
		}
		var err error
		if conf.Crt != nil && conf.Key != nil {
			hlog.Info(ctx, "Open https server, addr: %s", *conf.Addr)
			err = h.ServeTLS(listener, *conf.Crt, *conf.Key)
		} else {
			hlog.Info(ctx, "Open http server, addr: %s", *conf.Addr)
			err = h.Serve(listener)
		}
		if err != nil {
			if errors.Is(err, http.ErrServerClosed) {
				hlog.Info(ctx, "Server closed %s", *conf.Addr)
			} else {
				hlog.Error(ctx, "Listen server failed with '%s'\n", err)
			}
			_ = listener.Close()
			return
		}

		h = a.http.Swap(nil)
		if h != nil {
			_ = h.Close()
		}
	}()

	a.config.Store(conf)
	return nil
}

type ResponseWriter struct {
	writer http.ResponseWriter
	status int
}

func (r *ResponseWriter) Header() http.Header {
	return r.writer.Header()
}

func (r *ResponseWriter) Write(bytes []byte) (int, error) {
	return r.writer.Write(bytes)
}

func (r *ResponseWriter) WriteHeader(statusCode int) {
	r.status = statusCode
	r.writer.WriteHeader(statusCode)
}

func (r *ResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	h, ok := r.writer.(http.Hijacker)
	if !ok {
		return nil, nil, herror.NewError("the writer doesn't support the Hijacker interface")
	}
	return h.Hijack()
}

func (r *ResponseWriter) Flush() {
	if f, ok := r.writer.(http.Flusher); ok {
		f.Flush()
	}
}

// WithContext 给上下文添加 HTTP 连接
func WithContext(ctx context.Context, writer http.ResponseWriter, request *http.Request) context.Context {
	return &Context{
		Context: ctx,
		writer:  writer,
		request: request,
	}
}

// Context 定义了 HTTP 的上下文
type Context struct {
	context.Context
	writer  http.ResponseWriter
	request *http.Request
}

var contextType = reflect.TypeOf(&Context{})

// Value 返回Context的value
func (d *Context) Value(key any) any {
	if reflect.TypeOf(d) == key {
		return d
	}
	return d.Context.Value(key)
}

// FromContext 从上下文中获取 HTTP 连接
func FromContext(ctx context.Context) (writer http.ResponseWriter, request *http.Request, ok bool) {
	val := ctx.Value(contextType)
	if val == nil {
		return nil, nil, false
	}
	return val.(*Context).writer, val.(*Context).request, true
}

func (a *Http) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	//允许跨域
	writer.Header().Add("Access-Control-Allow-Origin", "*")
	writer.Header().Add("Access-Control-Allow-Headers", "*")
	writer.Header().Add("Access-Control-Allow-Methods", "*")
	//放行所有OPTIONS方法
	if request.Method == "OPTIONS" {
		writer.WriteHeader(http.StatusOK)
		return
	}

	start := time.Now()
	w := &ResponseWriter{
		writer: writer,
		status: http.StatusOK,
	}

	ctx := request.Context()
	if a.newContext != nil {
		ctx = a.newContext()
	}
	ctx = hlog.WithContext(ctx, request.Header.Get("X-Trace-Id"))

	a.mux.ServeHTTP(w, request.WithContext(WithContext(ctx, w, request)))
	old := time.Since(start) / time.Millisecond
	t := "[" + strconv.FormatFloat(float64(old), 'f', 3, 64) + "ms]"

	text := a.builderPool.Get().(*strings.Builder)
	text.Reset()
	defer a.builderPool.Put(text)

	//获得响应状态码
	text.WriteString("[")
	text.WriteString(hlog.FromContext(ctx))
	text.WriteString("] ")

	if 200 > old {
		text.WriteString(hutl.Yellow(t))
	} else {
		text.WriteString(hutl.Red(t))
	}

	text.WriteString(" ")
	httpIP, _ := hip.GetHttpIP(request)
	text.WriteString(httpIP)

	text.WriteString(" ")
	text.WriteString(request.Method)

	text.WriteString(" ")
	text.WriteString(request.Proto)

	text.WriteString(" ")
	text.WriteString(strconv.Itoa(w.status))

	text.WriteString(" ")
	text.WriteString(hutl.Green(request.URL.String()))

	_ = a.log.Output(1, LogHTTP, text.String())
}

// Shutdown 优雅关闭 HTTP 服务
func (a *Http) Shutdown(ctx context.Context) error {
	h := a.http.Load()
	if h == nil {
		return nil
	}
	hlog.Info(ctx, "http server closing")
	defer func() {
		hlog.Info(ctx, "http server closed")
	}()
	return h.Shutdown(ctx)
}
