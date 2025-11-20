package hrpc

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"golang.org/x/net/http2"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

// HttpClientOption  HttpClient 的选项。
type HttpClientOption func(*HttpClient)

// NewHttpClient 创建一个新的 HttpClient。
func NewHttpClient(base string, options ...HttpClientOption) *HttpClient {
	base = strings.TrimRight(base, "/") + "/"

	transport := &http.Transport{
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: true},
		MaxIdleConns:        50, // 限制最大空闲连接数
		IdleConnTimeout:     30 * time.Second,
		MaxIdleConnsPerHost: 100, // 限制每个主机的最大连接数
	}
	if strings.HasPrefix(base, "https://") {
		err := http2.ConfigureTransport(transport)
		if err != nil {
			os.Exit(1)
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
	err := h.middleware(func(ctx context.Context, path string, writer io.Writer, reader io.Reader, header http.Header) error {
		return h.server.Response(ctx, path, writer, reader, header)
	})(r.Context(), r.URL.Path[len(h.pathPrefix):], w, r.Body, r.Header)
	if err != nil {
		var e *Result[hbuf.Data]
		if errors.As(err, &e) && e.Code == -1 {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		herror.PrintStack(err)
	}
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
