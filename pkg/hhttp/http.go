package hhttp

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hip"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hutl"
	"net"
	"net/http"
	"reflect"
	"strconv"
	"time"
)

type Http struct {
	mux    http.ServeMux
	http   *http.Server
	config Config

	init   chan bool
	isInit bool
}

func NewHttp() *Http {
	ret := &Http{
		init: make(chan bool, 1),
	}
	ret.mux.HandleFunc("/health", ret.health)
	return ret
}

func (a *Http) Init() {
	a.isInit = true
	a.init <- true
}
func (a *Http) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	a.mux.HandleFunc(pattern, handler)
}

func (a *Http) Handle(pattern string, handler http.Handler) {
	a.mux.Handle(pattern, handler)
}

// SetConfig 设置配置
func (a *Http) SetConfig(conf *Config) error {
	if a.config.Equal(conf) {
		return nil
	}

	if a.http != nil {
		_ = a.http.Close()
		hlog.Info("close old http connection")
	}

	if nil == conf {
		a.http = nil
		return nil
	}

	listener, err := net.Listen("tcp", *conf.Addr)
	if err != nil {
		hlog.Error("Listen server failed with '%s'\n", err)
		return nil
	}

	a.http = &http.Server{
		Handler: a,
	}

	go func() {
		if !a.isInit {
			<-a.init
		}
		var err error
		if conf.Crt != nil && conf.Key != nil {
			hlog.Info("Start https server, addr: %s", *conf.Addr)
			err = a.http.ServeTLS(listener, *conf.Crt, *conf.Key)
		} else {
			hlog.Info("Start http server, addr: %s", *conf.Addr)
			err = a.http.Serve(listener)
		}
		if err != nil {
			if errors.Is(err, http.ErrServerClosed) {
				hlog.Info("Server closed %s", *conf.Addr)
			} else {
				hlog.Error("Listen server failed with '%s'\n", err)
			}
		}
	}()

	a.config = *conf
	return nil
}

func (a *Http) health(writer http.ResponseWriter, request *http.Request) {
	writer.WriteHeader(http.StatusOK)
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

	old := time.Now().UnixMilli()
	w := &ResponseWriter{
		writer: writer,
		status: http.StatusOK,
	}

	a.mux.ServeHTTP(w, request.WithContext(WithContext(request.Context(), w, request)))
	old = time.Now().UnixMilli() - old
	t := "[" + strconv.FormatFloat(float64(old)/1000, 'g', 3, 64) + "s]"
	if 200 > old {
		t = hutl.Yellow(t)
	} else {
		t = hutl.Red(t)
	}

	//获得响应状态码
	httpIP, _ := hip.GetHttpIP(request)
	_ = hlog.Output(1, LogHTTP, fmt.Sprintln(t, httpIP, request.Method, request.Proto, w.status, hutl.Green(request.URL.String())))
}
