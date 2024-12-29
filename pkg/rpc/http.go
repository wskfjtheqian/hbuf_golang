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

// NewHttpClient 创建一个新的 HttpClient。
func NewHttpClient(base string) *HttpClient {
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

	return &HttpClient{
		base: base,
		client: &http.Client{
			Transport: transport,
		},
	}

	return &HttpClient{}
}

// HttpClient 实现了 Invoke 方法，用于调用 HTTP 服务。
type HttpClient struct {
	base   string
	client *http.Client
}

func (h *HttpClient) Invoke(ctx context.Context, path string, writer io.Writer, reader io.Reader) error {
	if writer == nil {
		return errors.New("writer must not be nil")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.base+path, reader)
	if err != nil {
		return err
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return errors.New(resp.Status)
	}
	_, err = io.Copy(writer, resp.Body)
	if err != nil {
		return err
	}
	return nil
}

////////////////////////////////////////////////////////////////////////////

// NewHttpServer 创建一个新的 HttpServer。
func NewHttpServer(pathPrefix string, server *Server) *HttpServer {
	pathPrefix = "/" + strings.Trim(pathPrefix, "/") + "/"
	return &HttpServer{
		pathPrefix: pathPrefix,
		server:     server,
	}
}

// HttpServer 实现了 http.Handler 接口，用于处理 HTTP 请求。
type HttpServer struct {
	pathPrefix string
	server     *Server
}

// ServeHTTP 实现 http.Handler 接口。
func (h *HttpServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	err := h.server.Invoke(r.Context(), r.URL.Path[len(h.pathPrefix):], w, r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
