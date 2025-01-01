package rpc

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"golang.org/x/net/http2"
	"io"
	"net/http"
	"os"
	"reflect"
	"strings"
)

// WithHttpContext 在 context 中存储 HttpContext。
func WithHttpContext(ctx context.Context, writer http.ResponseWriter, request *http.Request) context.Context {
	return &HttpContext{
		Context: ctx,
		writer:  writer,
		request: request,
	}
}

// HttpContext Value 用于在 context 中存储 HttpContext。
type HttpContext struct {
	context.Context

	writer  http.ResponseWriter
	request *http.Request
}

var httpType = reflect.TypeOf(&HttpContext{})

func (d *HttpContext) Value(key any) any {
	if reflect.TypeOf(d) == key {
		return d
	}
	return d.Context.Value(key)
}

// FromHttpContext 从 context 中获取 HttpContext。
func FromHttpContext(ctx context.Context) *HttpContext {
	return ctx.Value(httpType).(*HttpContext)
}

////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

// HttpClientOption  HttpClient 的选项。
type HttpClientOption func(*HttpClient)

// NewHttpClient 创建一个新的 HttpClient。
func NewHttpClient(base string, options ...HttpClientOption) *HttpClient {
	base = strings.TrimRight(base, "/") + "/"

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
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
		filter: func(ctx context.Context, request Request) Request {
			return request
		},
	}

	for _, option := range options {
		option(ret)
	}
	return ret
}

// HttpClient 实现了 Request 方法，用于调用 HTTP 服务。
type HttpClient struct {
	base   string
	client *http.Client
	filter RequestFilter
}

func (h *HttpClient) Request(ctx context.Context, path string, notification bool, callback func(writer io.Writer) error) (io.ReadCloser, error) {
	return h.filter(ctx, func(ctx context.Context, path string, notification bool, callback func(writer io.Writer) error) (io.ReadCloser, error) {
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

// WithRequestFilter 设置请求过滤器。
func WithRequestFilter(filter RequestFilter) HttpClientOption {
	return func(h *HttpClient) {
		h.filter = filter
	}
}

////////////////////////////////////////////////////////////////////////////

// HttpServerOptions  HttpServer 的选项。
type HttpServerOptions func(*HttpServer)

// NewHttpServer 创建一个新的 HttpServer。
func NewHttpServer(pathPrefix string, server *Server, options ...HttpServerOptions) *HttpServer {
	pathPrefix = "/" + strings.Trim(pathPrefix, "/") + "/"
	ret := &HttpServer{
		pathPrefix: pathPrefix,
		server:     server,
		filter: func(ctx context.Context, response Response) Response {
			return response
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
	filter     ResponseFilter
}

// ServeHTTP 实现 http.Handler 接口。
func (h *HttpServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := WithHttpContext(r.Context(), w, r)
	err := h.filter(ctx, func(ctx context.Context, path string, writer io.Writer, reader io.Reader) error {
		return h.server.Response(ctx, path, writer, reader)
	})(ctx, r.URL.Path[len(h.pathPrefix):], w, r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

// WithResponseFilter 设置响应过滤器。
func WithResponseFilter(filter ResponseFilter) HttpServerOptions {
	return func(h *HttpServer) {
		h.filter = filter
	}
}
