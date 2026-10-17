package hrpc

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"golang.org/x/net/http2"
)

// HttpClientOption  HttpClient 的选项。
type HttpClientOption func(*HttpClient)

// NewHttpClient 创建一个新的 HttpClient。
func NewHttpClient(base string, options ...HttpClientOption) *HttpClient {
	base = strings.TrimRight(base, "/") + "/"

	transport := &http.Transport{
		TLSClientConfig:     &tls.Config{},
		MaxIdleConns:        50, // 限制最大空闲连接数
		IdleConnTimeout:     30 * time.Second,
		MaxIdleConnsPerHost: 100, // 限制每个主机的最大连接数
	}
	if strings.HasPrefix(base, "https://") {
		// HTTP/2 配置失败时降级为 HTTP/1.1 继续运行,不终止进程
		if err := http2.ConfigureTransport(transport); err != nil {
			hlog.Warn(context.Background(), "http2 configure fail, fallback to http/1.1: %v", err)
		}
	}

	ret := &HttpClient{
		base: base,
		client: &http.Client{
			Transport: transport,
		},
		middleware: func(next Request) Request {
			return next
		},
	}

	for _, option := range options {
		option(ret)
	}
	return ret
}

// HttpClient 实现了 Request 方法，用于调用 HTTP 服务。
type HttpClient struct {
	base       string
	client     *http.Client
	middleware RequestMiddleware
}

func (h *HttpClient) Request(ctx context.Context, path string, notification bool, callback func(writer io.Writer) error) (io.ReadCloser, error) {
	return h.middleware(func(ctx context.Context, path string, notification bool, callback func(writer io.Writer) error) (io.ReadCloser, error) {
		buffer := bytes.NewBuffer(nil)
		err := callback(buffer)
		if err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.base+path, buffer)
		if err != nil {
			return nil, err
		}

		rpcCtx := FromContext(ctx)
		if rpcCtx != nil {
			for k, v := range rpcCtx.header {
				for _, v2 := range v {
					req.Header.Add(k, v2)
				}
			}
		}

		resp, err := h.client.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != http.StatusOK {
			return nil, errors.New(resp.Status)
		}
		return resp.Body, nil
	})(ctx, path, notification, callback)
}

// WithRequestMiddleware 设置请求中间件。
func WithRequestMiddleware(middleware ...RequestMiddleware) HttpClientOption {
	return func(h *HttpClient) {
		h.middleware = func(next Request) Request {
			for i := len(middleware) - 1; i >= 0; i-- {
				next = middleware[i](next)
			}
			return next
		}
	}
}

// MaxIdleConnsPerHost 设置每个主机的最大空闲连接数限制
func MaxIdleConnsPerHost(maxConns int) HttpClientOption {
	return func(h *HttpClient) {
		h.client.Transport.(*http.Transport).MaxIdleConnsPerHost = maxConns
	}
}

// WithTimeout 设置HttpClient的超时时间
func WithTimeout(timeout time.Duration) HttpClientOption {
	return func(h *HttpClient) {
		h.client.Timeout = timeout
	}
}

// WithMaxConns 设置HttpClient的最大连接数
func WithMaxConns(maxConns int) HttpClientOption {
	return func(h *HttpClient) {
		h.client.Transport.(*http.Transport).MaxConnsPerHost = maxConns
	}
}

// WithConnTimeout 设置HttpClient的连接超时时间
func WithConnTimeout(connTimeout time.Duration) HttpClientOption {
	return func(h *HttpClient) {
		h.client.Transport.(*http.Transport).DialContext = (&net.Dialer{
			Timeout:   connTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext
	}
}

// WithMaxIdleConns 设置HttpClient的连接保持时间
func WithMaxIdleConns(maxIdleConns int) HttpClientOption {
	return func(h *HttpClient) {
		h.client.Transport.(*http.Transport).MaxIdleConns = maxIdleConns
	}
}

// WithIdleConnTimeout 设置HttpClient的每个主机的最大连接数
func WithIdleConnTimeout(idleConnTimeout time.Duration) HttpClientOption {
	return func(h *HttpClient) {
		h.client.Transport.(*http.Transport).IdleConnTimeout = idleConnTimeout
	}
}

// WithInsecureSkipVerify 跳过TLS证书校验(仅建议测试环境使用)
func WithInsecureSkipVerify(insecureSkipVerify bool) HttpClientOption {
	return func(h *HttpClient) {
		transport := h.client.Transport.(*http.Transport)
		if transport.TLSClientConfig == nil {
			transport.TLSClientConfig = &tls.Config{}
		}
		transport.TLSClientConfig.InsecureSkipVerify = insecureSkipVerify
	}
}

// WithTLSConfig 设置HttpClient的TLS配置
func WithTLSConfig(tlsConfig *tls.Config) HttpClientOption {
	return func(h *HttpClient) {
		h.client.Transport.(*http.Transport).TLSClientConfig = tlsConfig
	}
}

////////////////////////////////////////////////////////////////////////////

// HttpServerOptions  HttpServer 的选项。
type HttpServerOptions func(*HttpServer)

// NewHttpServer 创建一个新的 HttpServer。
func NewHttpServer(pathPrefix string, server *Server, options ...HttpServerOptions) *HttpServer {
	if pathPrefix != "/" {
		pathPrefix = "/" + strings.Trim(pathPrefix, "/") + "/"
	}
	ret := &HttpServer{
		pathPrefix: pathPrefix,
		server:     server,
		middleware: func(next Response) Response {
			return next
		},
	}

	for _, option := range options {
		option(ret)
	}
	return ret
}

// HttpServer 实现了 http.Handler 接口，用于处理 HTTP 请求。
type HttpServer struct {
	pathPrefix string
	server     *Server
	middleware ResponseMiddleware
}

// ServeHTTP 实现 http.Handler 接口。
func (h *HttpServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	ctx := r.Context()
	err := h.middleware(func(ctx context.Context, path string, writer io.Writer, reader io.Reader, header http.Header) error {
		return h.server.Response(ctx, path, writer, reader, header)
	})(ctx, r.URL.Path[len(h.pathPrefix):], w, r.Body, r.Header)
	if err != nil {
		status, serverError := StatusFromError(err)
		w.WriteHeader(status)
		if serverError {
			herror.PrintStack(ctx, err)
		}
	}
}

// Shutdown 等待在途请求完成并执行服务端注册的关闭钩子
func (h *HttpServer) Shutdown(ctx context.Context) error {
	return h.server.Shutdown(ctx)
}

// WithResponseMiddleware 设置响应中间件。
func WithResponseMiddleware(middleware ...ResponseMiddleware) HttpServerOptions {
	return func(h *HttpServer) {
		h.middleware = func(next Response) Response {
			for i := len(middleware) - 1; i >= 0; i-- {
				next = middleware[i](next)
			}
			return next
		}
	}
}
