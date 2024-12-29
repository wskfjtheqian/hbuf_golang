package rpc

import (
	"context"
	"crypto/tls"
	"errors"
	"golang.org/x/net/http2"
	"io"
	"net/http"
	"os"
	"strings"
)

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

// HttpClient 实现了 Invoke 方法，用于调用 HTTP 服务。
type HttpClient struct {
	base   string
	client *http.Client
	filter RequestFilter
}

func (h *HttpClient) Invoke(ctx context.Context, path string, reader io.Reader) (io.ReadCloser, error) {
	return h.filter(ctx, func(ctx context.Context, path string, reader io.Reader) (io.ReadCloser, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.base+path, reader)
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
	})(ctx, path, reader)
}

func WithRequestFilter(filter RequestFilter) HttpClientOption {
	return func(h *HttpClient) {
		h.filter = filter
	}
}

// //////////////////////////////////////////////////////////////////////////
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

	err := h.filter(r.Context(), func(ctx context.Context, writer io.Writer, reader io.Reader) error {
		err := h.server.Response(r.Context(), r.URL.Path[len(h.pathPrefix):], writer, reader)
		if err != nil {
			return err
		}
		return nil
	})(r.Context(), w, r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func WithResponseFilter(filter ResponseFilter) HttpServerOptions {
	return func(h *HttpServer) {
		h.filter = filter
	}
}
