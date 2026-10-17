package hrpc_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hrpc"
	"golang.org/x/net/http2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type TestHbufService struct {
}

func (t TestHbufService) HbufStream(ctx context.Context, reader io.Reader) (io.ReadCloser, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	hlog.Info(ctx, "receive data: %s", string(data))
	return io.NopCloser(bytes.NewReader(data)), nil
}

func (t TestHbufService) Init(ctx context.Context) {
}

func (t TestHbufService) HbufMethod(ctx context.Context, req *HbufRequest) (*HbufResponse, error) {
	return &HbufResponse{Name: req.Name, Age: req.Age}, nil
}

type TestProtoServiceServer struct {
	UnimplementedProtoServiceServer
}

func (t TestProtoServiceServer) ProtoMethod(ctx context.Context, request *ProtoRequest) (*ProtoResponse, error) {
	return &ProtoResponse{Name: request.Name, Age: request.Age}, nil
}

// 测试 HttpService 的 Response 方法
func TestHttpService_Invoke(t *testing.T) {
	rpcServer := hrpc.NewServer()
	RegisterHbufService(rpcServer, &TestHbufService{})

	server := hrpc.NewHttpServer("/rpc/", rpcServer)

	http.Handle("/rpc/", server)
	go http.ListenAndServe(":8080", nil)

	client := hrpc.NewHttpClient("http://localhost:8080/rpc")

	rpcClient := hrpc.NewClient(client.Request)
	testClient := NewHbufServiceClient(rpcClient)
	resp, err := testClient.HbufMethod(context.Background(), &HbufRequest{Name: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Name != "test" {
		t.Fatal("test fail")
	}

	stream, err := testClient.HbufStream(context.Background(), bytes.NewReader([]byte("HbufStream Test")))
	if err != nil {
		t.Fatal(err)
		return
	}
	defer stream.Close()
	data, err := io.ReadAll(stream)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "HbufStream Test" {
		t.Fatal("test fail")
	}
}

// 测试 HttpService 的 Response 方法
func TestHttpService_InvokeHBuf(t *testing.T) {
	rpcServer := hrpc.NewServer(hrpc.WithServerEncoder(hrpc.NewHBufEncode()), hrpc.WithServerDecode(hrpc.NewHBufDecode()))
	RegisterHbufService(rpcServer, &TestHbufService{})

	server := hrpc.NewHttpServer("/rpc/", rpcServer)

	http.Handle("/rpc/", server)
	go http.ListenAndServe(":8080", nil)

	client := hrpc.NewHttpClient("http://localhost:8080/rpc")

	rpcClient := hrpc.NewClient(client.Request, hrpc.WithClientEncoder(hrpc.NewHBufEncode()), hrpc.WithClientDecode(hrpc.NewHBufDecode()))
	testClient := NewHbufServiceClient(rpcClient)
	resp, err := testClient.HbufMethod(context.Background(), &HbufRequest{Name: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Name != "test" {
		t.Fatal("test fail")
	}

	stream, err := testClient.HbufStream(context.Background(), bytes.NewReader([]byte("HbufStream Test")))
	if err != nil {
		t.Error(err)
		return
	}
	defer stream.Close()
	data, err := io.ReadAll(stream)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "HbufStream Test" {
		t.Fatal("test fail")
	}
}

// 测试 HttpService 加密通信
func TestHttpService_Encoder(t *testing.T) {
	rpcServer := hrpc.NewServer(hrpc.WithServerEncoder(hrpc.NewHBufEncode()), hrpc.WithServerDecode(hrpc.NewHBufDecode()))
	RegisterHbufService(rpcServer, &TestHbufService{})

	server := hrpc.NewHttpServer("/rpc/", rpcServer, hrpc.WithResponseMiddleware(func(next hrpc.Response) hrpc.Response {
		return func(ctx context.Context, path string, writer io.Writer, reader io.Reader, header http.Header) error {
			decoder := base64.NewDecoder(base64.StdEncoding, reader)
			encoder := base64.NewEncoder(base64.StdEncoding, writer)
			defer encoder.Close()
			return next(ctx, path, encoder, decoder, header)
		}
	}))

	http.Handle("/rpc/", server)
	go http.ListenAndServe(":8080", nil)

	client := hrpc.NewHttpClient("http://localhost:8080/rpc", hrpc.WithRequestMiddleware(func(next hrpc.Request) hrpc.Request {
		return func(ctx context.Context, path string, notification bool, callback func(writer io.Writer) error) (io.ReadCloser, error) {
			body, err := next(ctx, path, notification, func(writer io.Writer) error {
				encoder := base64.NewEncoder(base64.StdEncoding, writer)
				defer encoder.Close()

				return callback(encoder)
			})

			if err != nil {
				return nil, err
			}
			decoder := base64.NewDecoder(base64.StdEncoding, body)
			return io.NopCloser(decoder), nil
		}
	}))

	rpcClient := hrpc.NewClient(client.Request, hrpc.WithClientEncoder(hrpc.NewHBufEncode()), hrpc.WithClientDecode(hrpc.NewHBufDecode()))
	testClient := NewHbufServiceClient(rpcClient)
	resp, err := testClient.HbufMethod(context.Background(), &HbufRequest{Name: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Name != "test" {
		t.Fatal("test fail")
	}

	stream, err := testClient.HbufStream(context.Background(), bytes.NewReader([]byte("HbufStream Test")))
	if err != nil {
		t.Error(err)
		return
	}
	defer stream.Close()
	data, err := io.ReadAll(stream)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "HbufStream Test" {
		t.Fatal("test fail")
	}
}

// 测试 base64
func TestBase64(t *testing.T) {
	writer := bytes.NewBuffer(nil)
	encoder := base64.NewEncoder(base64.StdEncoding, writer)
	defer encoder.Close()

	encoder.Write([]byte("adfasdfasdfasdfsa"))
}

// 测试 HttpService 的 Middleware 方法
func TestHttpService_Middleware(t *testing.T) {
	rpcServer := hrpc.NewServer(hrpc.WithServerMiddleware(func(next hrpc.Handler) hrpc.Handler {
		return func(ctx context.Context, req any) (any, error) {
			return next(ctx, req)
		}
	}))
	RegisterHbufService(rpcServer, &TestHbufService{})

	server := hrpc.NewHttpServer("/rpc/", rpcServer)

	http.Handle("/rpc/", server)
	go http.ListenAndServe(":8080", nil)

	client := hrpc.NewHttpClient("http://localhost:8080/rpc")

	rpcClient := hrpc.NewClient(client.Request)
	testClient := NewHbufServiceClient(rpcClient)
	resp, err := testClient.HbufMethod(context.Background(), &HbufRequest{Name: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Name != "test" {
		t.Fatal("test fail")
	}
	stream, err := testClient.HbufStream(context.Background(), bytes.NewReader([]byte("HbufStream Test")))
	if err != nil {
		t.Error(err)
		return
	}
	defer stream.Close()
	data, err := io.ReadAll(stream)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "HbufStream Test" {
		t.Fatal("test fail")
	}
}

func TestHttpService_Http2(t *testing.T) {
	rpcServer := hrpc.NewServer()
	RegisterHbufService(rpcServer, &TestHbufService{})

	server := hrpc.NewHttpServer("/rpc/", rpcServer)

	http.Handle("/rpc/", server)
	go func() {
		err := http.ListenAndServeTLS(
			":9080",
			"/Users/dev/2.hbuf/hbuf_golang/pkg/rpc/server.crt",
			"/Users/dev/2.hbuf/hbuf_golang/pkg/rpc/server.key",
			nil)
		if err != nil {
			t.Error(err)
			return
		}
	}()

	client := hrpc.NewHttpClient("https://localhost:9080/rpc", hrpc.WithInsecureSkipVerify(true))

	rpcClient := hrpc.NewClient(client.Request)
	testClient := NewHbufServiceClient(rpcClient)
	<-time.After(time.Second * 5)

	await := sync.WaitGroup{}
	for i := 0; i < 2; i++ {
		await.Add(1)
		go func() {
			resp, err := testClient.HbufMethod(context.Background(), &HbufRequest{Name: "test"})
			if err != nil {
				t.Error(err)
				return
			}
			if resp.Name != "test" {
				t.Error("test fail")
				return
			}
			t.Log("test success")
			await.Done()
		}()
	}
	await.Wait()

}

// 自签名证书
func TestHttpService_Middleware2(t *testing.T) {

	// 生成 ECDSA 私钥
	var generatePrivateKey = func() (*ecdsa.PrivateKey, error) {
		return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	}

	// 自签名证书
	var generateSelfSignedCert = func(privateKey *ecdsa.PrivateKey) (tls.Certificate, error) {
		// 填写自签名证书的信息
		template := x509.Certificate{
			SerialNumber: big.NewInt(1),
			Subject: pkix.Name{
				Organization: []string{"Fitten Tech"},
			},
			NotBefore:             time.Now(),
			NotAfter:              time.Now().AddDate(1, 0, 0),
			SubjectKeyId:          []byte{1, 2, 3, 4, 6},
			KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
			ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
			IsCA:                  false,
			BasicConstraintsValid: true,
			DNSNames:              []string{"localhost"},
		}

		// 自签名证书
		certBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &privateKey.PublicKey, privateKey)
		if err != nil {
			return tls.Certificate{}, err
		}

		// 创建 TLS 证书
		cert := tls.Certificate{
			Certificate: [][]byte{certBytes},
			PrivateKey:  privateKey,
		}

		return cert, nil
	}

	rpcServer := hrpc.NewServer()
	RegisterHbufService(rpcServer, &TestHbufService{})

	server := hrpc.NewHttpServer("/rpc/", rpcServer)

	http.Handle("/rpc/", server)
	go func() {
		// 1. 生成私钥
		privateKey, err := generatePrivateKey()
		if err != nil {
			hlog.Error(hlog.NewContext(), "generate private key failed: %s", err)
			return
		}

		// 5. 生成自签名证书
		cert, err := generateSelfSignedCert(privateKey)
		if err != nil {
			hlog.Error(hlog.NewContext(), "generate self signed cert failed: %s", err)
			return
		}

		server := &http.Server{
			Addr:    ":9080",
			Handler: http.DefaultServeMux,
			TLSConfig: &tls.Config{
				Certificates: []tls.Certificate{cert},
			},
		}
		err = server.ListenAndServeTLS("", "")
		if err != nil {
			t.Error(err)
			return
		}
	}()

	client := hrpc.NewHttpClient("https://localhost:9080/rpc", hrpc.WithInsecureSkipVerify(true))

	rpcClient := hrpc.NewClient(client.Request)
	testClient := NewHbufServiceClient(rpcClient)
	resp, err := testClient.HbufMethod(context.Background(), &HbufRequest{Name: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Name != "test" {
		t.Fatal("test fail")
	}
}

// 测试 HttpService 的性能
func Benchmark_HRPC_HTTP(b *testing.B) {
	rpcServer8 := hrpc.NewServer(hrpc.WithServerEncoder(hrpc.NewHBufEncode()), hrpc.WithServerDecode(hrpc.NewHBufDecode()))
	RegisterHbufService(rpcServer8, &TestHbufService{})

	server8 := hrpc.NewHttpServer("/rpc/", rpcServer8)

	mux8 := http.NewServeMux()
	mux8.Handle("/rpc/", server8)
	go http.ListenAndServe(":8180", mux8)

	client8 := hrpc.NewHttpClient("http://localhost:8180/rpc")
	rpcClient8 := hrpc.NewClient(client8.Request, hrpc.WithClientEncoder(hrpc.NewHBufEncode()), hrpc.WithClientDecode(hrpc.NewHBufDecode()))
	testClient8 := NewHbufServiceClient(rpcClient8)

	b.Run("HRPC_HTTP_HBufEncode", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			resp, err := testClient8.HbufMethod(context.Background(), &HbufRequest{Name: "test"})
			if err != nil {
				b.Fatal(err)
			}
			if resp.Name != "test" {
				b.Fatal("test fail")
			}
		}
	})

	rpcServer := hrpc.NewServer(hrpc.WithServerEncoder(hrpc.NewHBufEncode()), hrpc.WithServerDecode(hrpc.NewHBufDecode()))
	RegisterHbufService(rpcServer, &TestHbufService{})

	server := hrpc.NewHttpServer("/rpc/", rpcServer)

	mux := http.NewServeMux()
	mux.Handle("/rpc/", server)
	go http.ListenAndServe(":8080", mux)

	client := hrpc.NewHttpClient("http://localhost:8080/rpc")
	rpcClient := hrpc.NewClient(client.Request, hrpc.WithClientEncoder(hrpc.NewHBufEncode()), hrpc.WithClientDecode(hrpc.NewHBufDecode()))
	testClient := NewHbufServiceClient(rpcClient)

	b.Run("HRPC_HTTP", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			resp, err := testClient.HbufMethod(context.Background(), &HbufRequest{Name: "test"})
			if err != nil {
				b.Fatal(err)
			}
			if resp.Name != "test" {
				b.Fatal("test fail")
			}
		}
	})

	rpcServer1 := hrpc.NewServer(hrpc.WithServerEncoder(hrpc.NewHBufEncode()), hrpc.WithServerDecode(hrpc.NewHBufDecode()))
	RegisterHbufService(rpcServer1, &TestHbufService{})

	server1 := hrpc.NewHttpServer("/rpc/", rpcServer1)

	mux1 := http.NewServeMux()
	mux1.Handle("/rpc/", server1)
	go http.ListenAndServeTLS(":8081", "/Users/dev/2.hbuf/hbuf_golang/pkg/hrpc/server.crt", "/Users/dev/2.hbuf/hbuf_golang/pkg/hrpc/server.key", mux1)

	client1 := hrpc.NewHttpClient("https://localhost:8081/rpc", hrpc.WithInsecureSkipVerify(true))
	rpcClient1 := hrpc.NewClient(client1.Request, hrpc.WithClientEncoder(hrpc.NewHBufEncode()), hrpc.WithClientDecode(hrpc.NewHBufDecode()))
	testClient1 := NewHbufServiceClient(rpcClient1)

	b.Run("HRPC_HTTPS", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			resp, err := testClient1.HbufMethod(context.Background(), &HbufRequest{Name: "test"})
			if err != nil {
				b.Fatal(err)
			}
			if resp.Name != "test" {
				b.Fatal("test fail")
			}
		}
	})

	rpcServer2 := hrpc.NewServer()
	RegisterHbufService(rpcServer2, &TestHbufService{})

	server2 := hrpc.NewWebSocketServer(rpcServer2.Response, hrpc.WithWebSocketServerEncode(hrpc.NewHBufEncode()), hrpc.WithWebSocketServerDecode(hrpc.NewHBufDecode()))

	mux2 := http.NewServeMux()
	mux2.HandleFunc("/socket", func(writer http.ResponseWriter, request *http.Request) {
		server2.ServeHTTP(writer, request, "12", nil, nil)
	})
	go http.ListenAndServe(":8084", mux2)

	client2 := hrpc.NewWebSocketClient("ws://localhost:8084/socket", nil, hrpc.WithWebSocketClientEncode(hrpc.NewHBufEncode()), hrpc.WithWebSocketClientDecode(hrpc.NewHBufDecode()))

	err := client2.Connect(context.Background())
	if err != nil {
		b.Fatal(err)
	}
	rpcClient2 := hrpc.NewClient(client2.Request)
	testClient2 := NewHbufServiceClient(rpcClient2)
	//<-time.After(time.Second * 1)
	b.Run("HRPC_WS", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			resp, err := testClient2.HbufMethod(context.Background(), &HbufRequest{Name: "test"})
			if err != nil {
				b.Fatal(err)
			}
			if resp.Name != "test" {
				b.Fatal("test fail")
			}
		}
	})

	rpcServer3 := hrpc.NewServer()
	RegisterHbufService(rpcServer3, &TestHbufService{})

	server3 := hrpc.NewWebSocketServer(rpcServer3.Response)

	mux3 := http.NewServeMux()
	mux3.HandleFunc("/socket", func(writer http.ResponseWriter, request *http.Request) {
		server3.ServeHTTP(writer, request, "12", nil, nil)
	})
	go http.ListenAndServeTLS(":8085", "/Users/dev/2.hbuf/hbuf_golang/pkg/hrpc/server.crt", "/Users/dev/2.hbuf/hbuf_golang/pkg/hrpc/server.key", mux3)

	client3 := hrpc.NewWebSocketClient("wss://localhost:8085/socket", nil, hrpc.WithWebSocketClientInsecureSkipVerify(true))

	err = client3.Connect(context.Background())
	if err != nil {
		b.Fatal(err)
	}
	rpcClient3 := hrpc.NewClient(client3.Request)
	testClient3 := NewHbufServiceClient(rpcClient3)
	//<-time.After(time.Second * 1)
	b.Run("HRPC_WSS", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			resp, err := testClient3.HbufMethod(context.Background(), &HbufRequest{Name: "test"})
			if err != nil {
				b.Fatal(err)
			}
			if resp.Name != "test" {
				b.Fatal("test fail")
			}
		}
	})

	go http.ListenAndServe(":8082", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := &HbufRequest{}
		err := json.NewDecoder(r.Body).Decode(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp := &HbufResponse{Name: req.Name}
		json.NewEncoder(w).Encode(resp)
	}))
	b.Run("Http", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			req := &HbufRequest{
				Name: "test",
			}
			body := bytes.NewBuffer(nil)
			err := json.NewEncoder(body).Encode(req)
			if err != nil {
				b.Fatal(err)
				return
			}
			post, err := http.Post("http://localhost:8082", "application/json", body)
			if err != nil {
				b.Fatal(err)
				return
			}
			defer post.Body.Close()
			resp := &HbufResponse{Name: req.Name}
			err = json.NewDecoder(post.Body).Decode(resp)
			if err != nil {
				b.Fatal(err)
				return
			}
			if resp.Name != "test" {
				b.Fatal("test fail")
				return
			}
		}
	})

	go http.ListenAndServeTLS(":8083", "/Users/dev/2.hbuf/hbuf_golang/pkg/hrpc/server.crt", "/Users/dev/2.hbuf/hbuf_golang/pkg/hrpc/server.key", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		req := &HbufRequest{}
		err := json.NewDecoder(r.Body).Decode(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp := &HbufResponse{Name: req.Name}
		json.NewEncoder(w).Encode(resp)
	}))

	client6 := &http.Client{
		Transport: &http2.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}
	b.Run("Https", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			req := &HbufRequest{
				Name: "test",
			}
			body := bytes.NewBuffer(nil)
			err := json.NewEncoder(body).Encode(req)
			if err != nil {
				b.Fatal(err)
				return
			}
			post, err := client6.Post("https://localhost:8083", "application/json", body)
			if err != nil {
				b.Fatal(err)
				return
			}
			defer post.Body.Close()
			resp := &HbufResponse{Name: req.Name}
			err = json.NewDecoder(post.Body).Decode(resp)
			if err != nil {
				b.Fatal(err)
				return
			}
			if resp.Name != "test" {
				b.Fatal("test fail")
				return
			}
		}
	})

	listen, err := net.Listen("tcp", ":8086")
	if err != nil {
		b.Fatal(err)
		return
	}
	rpcServer4 := grpc.NewServer()
	RegisterProtoServiceServer(rpcServer4, &TestProtoServiceServer{})
	go rpcServer4.Serve(listen)

	newClient4, err := grpc.NewClient("localhost:8086", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		b.Fatal(err)
		return
	}
	testClient4 := NewProtoServiceClient(newClient4)
	_, err = testClient4.ProtoMethod(context.Background(), &ProtoRequest{Name: "test", Age: 18})
	if err != nil {
		b.Fatal(err)
		return
	}
	b.Run("GRPC", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			resp, err := testClient4.ProtoMethod(context.Background(), &ProtoRequest{Name: "test", Age: 18})
			if err != nil {
				b.Fatal(err)
				return
			}
			if resp.Name != "test" {
				b.Fatal("test fail")
				return
			}
		}
	})
}

type ProtoServiceConcurrency struct {
	UnimplementedProtoServiceServer
	count atomic.Int32
}

func (p *ProtoServiceConcurrency) ProtoMethod(ctx context.Context, req *ProtoRequest) (*ProtoResponse, error) {
	p.count.Add(1)
	return &ProtoResponse{Name: req.Name, Age: req.Age}, nil
}

type HbufServiceConcurrency struct {
	count atomic.Int32
}

func (h *HbufServiceConcurrency) HbufStream(ctx context.Context, reader io.Reader) (io.ReadCloser, error) {
	//TODO implement me
	panic("implement me")
}

func (h *HbufServiceConcurrency) Init(ctx context.Context) {

}

func (h *HbufServiceConcurrency) HbufMethod(ctx context.Context, req *HbufRequest) (*HbufResponse, error) {
	h.count.Add(1)
	return &HbufResponse{Name: req.Name, Age: req.Age}, nil
}

// 测试并发性能
func Test_Concurrency(t *testing.T) {
	var now atomic.Int64
	now.Store(time.Now().UnixMilli())
	go func() {
		for {
			temp := <-time.After(time.Second * 1)
			now.Store(temp.UnixMilli())
		}
	}()

	count := atomic.Int32{}

	go http.ListenAndServe(":8089", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		count.Add(1)
		req := &HbufRequest{}
		err := json.NewDecoder(r.Body).Decode(req)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp := &HbufResponse{Name: req.Name}
		json.NewEncoder(w).Encode(resp)
	}))

	<-time.After(time.Second * 1)

	timeLength := 10
	end := now.Load() + int64(time.Duration(timeLength)*time.Second/time.Millisecond)
	var wgHttp sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wgHttp.Add(1)
		go func() {
			defer wgHttp.Done()
			for end > now.Load() {
				req := &HbufRequest{
					Name: "test",
				}
				body := bytes.NewBuffer(nil)
				err := json.NewEncoder(body).Encode(req)
				if err != nil {
					t.Fatal(err)
					return
				}
				post, err := http.Post("http://localhost:8089", "application/json", body)
				if err != nil {
					t.Fatal(err)
					return
				}
				defer post.Body.Close()
				resp := &HbufResponse{Name: req.Name}
				err = json.NewDecoder(post.Body).Decode(resp)
				if err != nil {
					t.Fatal(err)
					return
				}
				if resp.Name != "test" {
					t.Fatal("test fail")
					return
				}
			}
		}()
	}
	wgHttp.Wait()
	t.Log("http concurrency count: per second ", count.Load()/int32(timeLength))

	grpcListener, err := net.Listen("tcp", ":8080")
	if err != nil {
		t.Fatal(err)
		return
	}

	grpcService := &ProtoServiceConcurrency{}
	grpcServer := grpc.NewServer()
	RegisterProtoServiceServer(grpcServer, grpcService)
	go grpcServer.Serve(grpcListener)

	grpcClient, err := grpc.NewClient("localhost:8080", grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
		return
	}
	grpcTestClient := NewProtoServiceClient(grpcClient)

	_, err = grpcTestClient.ProtoMethod(context.Background(), &ProtoRequest{Name: "test " + strconv.Itoa(-1), Age: 18})
	if err != nil {
		t.Error(err)
		return
	}

	timeLength = 10
	end = now.Load() + int64(time.Duration(timeLength)*time.Second/time.Millisecond)

	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for end > now.Load() {
				_, err := grpcTestClient.ProtoMethod(context.Background(), &ProtoRequest{Name: "test " + strconv.Itoa(i), Age: 18})
				if err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg.Wait()
	t.Log("grpc concurrency count: per second ", grpcService.count.Load()/int32(timeLength))

	hrpcService := &HbufServiceConcurrency{}
	hRpcServer := hrpc.NewServer(hrpc.WithServerEncoder(hrpc.NewHBufEncode()), hrpc.WithServerDecode(hrpc.NewHBufDecode()))
	RegisterHbufService(hRpcServer, hrpcService)
	mux := http.NewServeMux()
	mux.Handle("/rpc/", hrpc.NewHttpServer("/rpc/", hRpcServer))

	go http.ListenAndServeTLS(":8081", "/Users/dev/2.hbuf/hbuf_golang/pkg/hrpc/server.crt", "/Users/dev/2.hbuf/hbuf_golang/pkg/hrpc/server.key", mux)

	httpClient := hrpc.NewHttpClient("https://localhost:8081/rpc", hrpc.WithInsecureSkipVerify(true))
	rpcClient := hrpc.NewClient(httpClient.Request, hrpc.WithClientEncoder(hrpc.NewHBufEncode()), hrpc.WithClientDecode(hrpc.NewHBufDecode()))
	rpcTestClient := NewHbufServiceClient(rpcClient)

	_, err = rpcTestClient.HbufMethod(context.Background(), &HbufRequest{Name: "test " + strconv.Itoa(-1)})
	if err != nil {
		t.Error(err)
		return
	}
	<-time.After(time.Second * 1)
	timeLength = 10
	end = now.Load() + int64(time.Duration(timeLength)*time.Second/time.Millisecond)

	var wg1 sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg1.Add(1)
		go func() {
			defer wg1.Done()
			for end > now.Load() {
				_, err := rpcTestClient.HbufMethod(context.Background(), &HbufRequest{Name: "test " + strconv.Itoa(i)})
				if err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	wg1.Wait()
	t.Log("HRPC_HTTP concurrency count: per second ", hrpcService.count.Load()/int32(timeLength))

	{
		// 测试 HRPC_WS 并发性能
		wsService := &HbufServiceConcurrency{}
		rpcServer3 := hrpc.NewServer(hrpc.WithServerEncoder(hrpc.NewHBufEncode()), hrpc.WithServerDecode(hrpc.NewHBufDecode()))
		RegisterHbufService(rpcServer3, wsService)

		wsServer := hrpc.NewWebSocketServer(rpcServer3.Response, hrpc.WithWebSocketServerEncode(hrpc.NewHBufEncode()), hrpc.WithWebSocketServerDecode(hrpc.NewHBufDecode()))

		mux := http.NewServeMux()
		mux.HandleFunc("/socket", func(writer http.ResponseWriter, request *http.Request) {
			wsServer.ServeHTTP(writer, request, "12", nil, nil)
		})
		go http.ListenAndServeTLS(":8082", "/Users/dev/2.hbuf/hbuf_golang/pkg/hrpc/server.crt", "/Users/dev/2.hbuf/hbuf_golang/pkg/hrpc/server.key", mux)

		timeLength := 10
		var wg sync.WaitGroup
		for i := 0; i < 33; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				wsClient := hrpc.NewWebSocketClient("wss://localhost:8082/socket", nil, hrpc.WithWebSocketClientInsecureSkipVerify(true), hrpc.WithWebSocketClientEncode(hrpc.NewHBufEncode()), hrpc.WithWebSocketClientDecode(hrpc.NewHBufDecode()))
				wsRpcClient := hrpc.NewClient(wsClient.Request, hrpc.WithClientEncoder(hrpc.NewHBufEncode()), hrpc.WithClientDecode(hrpc.NewHBufDecode()))
				wsTestClient := NewHbufServiceClient(wsRpcClient)
				err := wsClient.Connect(context.Background())
				if err != nil {
					t.Fatal(err)
				}

				_, err = wsTestClient.HbufMethod(context.Background(), &HbufRequest{Name: "test " + strconv.Itoa(-1)})
				if err != nil {
					t.Error(err)
					return
				}

				end := now.Load() + int64(time.Duration(timeLength)*time.Second/time.Millisecond)

				var wg2 sync.WaitGroup
				for i := 0; i < 33; i++ {
					wg2.Add(1)
					go func() {
						defer wg2.Done()
						for end > now.Load() {
							_, err := wsTestClient.HbufMethod(context.Background(), &HbufRequest{Name: "test " + strconv.Itoa(i)})
							if err != nil {
								t.Error(err)
								return
							}
						}
					}()
				}
				wg2.Wait()
			}()
		}
		wg.Wait()
		t.Log("HRPC_WS concurrency count: per second ", wsService.count.Load()/int32(timeLength))
	}

	{
		// 测试全自研 h2 链路(H2FramerTransport + H2FramerServer)并发性能
		framerService := &HbufServiceConcurrency{}
		framerRpcServer := hrpc.NewServer(hrpc.WithServerEncoder(hrpc.NewHBufEncode()), hrpc.WithServerDecode(hrpc.NewHBufDecode()))
		RegisterHbufService(framerRpcServer, framerService)
		framerAddr := newH2FramerServer(t, framerRpcServer)

		framerTransport := hrpc.NewH2FramerTransport()
		defer framerTransport.CloseIdleConnections()
		framerHttpClient := hrpc.NewHttpClient("http://"+framerAddr+"/rpc", hrpc.WithH2FramerTransport(framerTransport))
		framerRpcClient := hrpc.NewClient(framerHttpClient.Request, hrpc.WithClientEncoder(hrpc.NewHBufEncode()), hrpc.WithClientDecode(hrpc.NewHBufDecode()))
		framerTestClient := NewHbufServiceClient(framerRpcClient)

		_, err := framerTestClient.HbufMethod(context.Background(), &HbufRequest{Name: "test " + strconv.Itoa(-1)})
		if err != nil {
			t.Error(err)
			return
		}
		timeLength := 10
		end := now.Load() + int64(time.Duration(timeLength)*time.Second/time.Millisecond)
		var wg sync.WaitGroup
		for i := 0; i < 1000; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for end > now.Load() {
					_, err := framerTestClient.HbufMethod(context.Background(), &HbufRequest{Name: "test " + strconv.Itoa(i)})
					if err != nil {
						t.Error(err)
						return
					}
				}
			}()
		}
		wg.Wait()
		t.Log("HRPC_H2Framer+FramerServer concurrency count: per second ", framerService.count.Load()/int32(timeLength))
	}

}

// 测试 traceId 从 header 透传到 handler
func TestHttpService_TraceId(t *testing.T) {
	traceIds := make(chan string, 1)
	rpcServer := hrpc.NewServer()
	rpcServer.Register(0, "test", nil, &hrpc.Method{
		Name: "trace",
		WithContext: func(ctx context.Context) context.Context {
			return ctx
		},
		Handler: func(ctx context.Context, req any) (any, error) {
			traceIds <- hlog.FromContext(ctx)
			return &HbufResponse{}, nil
		},
	})

	header := http.Header{}
	header.Set("X-Trace-Id", "trace-header-123")
	err := rpcServer.Response(context.Background(), "test/trace", io.Discard, nil, header)
	if err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-traceIds:
		if got != "trace-header-123" {
			t.Fatalf("traceId mismatch: got %q, want trace-header-123", got)
		}
	case <-time.After(time.Second):
		t.Fatal("handler not called")
	}
}

// 测试 handler 返回 nil 数据时不再 panic,返回明确错误
func TestServer_ResponseNilData(t *testing.T) {
	rpcServer := hrpc.NewServer()
	rpcServer.Register(0, "test", nil, &hrpc.Method{
		Name: "nil",
		WithContext: func(ctx context.Context) context.Context {
			return ctx
		},
		Handler: func(ctx context.Context, req any) (any, error) {
			return nil, nil
		},
	})

	err := rpcServer.Response(context.Background(), "test/nil", io.Discard, nil, http.Header{})
	if err == nil {
		t.Fatal("expected error for nil response")
	}
}

type testInvoke struct{}

func (t testInvoke) OnlyError(ctx context.Context) error {
	return nil
}

func (t testInvoke) BadSingle(ctx context.Context) string {
	return "bad"
}

func (t testInvoke) DataAndError(ctx context.Context) (string, error) {
	return "ok", nil
}

// 测试 InvokeServerFunc 的返回值约定
func TestInvokeServerFunc(t *testing.T) {
	obj := testInvoke{}

	if _, err := hrpc.InvokeServerFunc(context.Background(), obj, "OnlyError", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := hrpc.InvokeServerFunc(context.Background(), obj, "BadSingle", ""); err == nil {
		t.Fatal("expected error for non-error single return")
	}
	val, err := hrpc.InvokeServerFunc(context.Background(), obj, "DataAndError", "")
	if err != nil {
		t.Fatal(err)
	}
	if val != "ok" {
		t.Fatal("unexpected data")
	}
}

////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////
// H2FramerTransport 与 gRPC 对比

// newGrpcServer 启动 gRPC 测试服务端(动态端口),返回监听地址。
// 并发流上限设大,与 H2FramerTransport 同为单连接承载全部并发流。
func newGrpcServer(tb testing.TB) string {
	tb.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		tb.Fatal(err)
	}
	grpcServer := grpc.NewServer(grpc.MaxConcurrentStreams(10000))
	RegisterProtoServiceServer(grpcServer, &TestProtoServiceServer{})
	go grpcServer.Serve(listener)
	tb.Cleanup(func() {
		grpcServer.Stop()
		_ = listener.Close()
	})
	return listener.Addr().String()
}

// newStdH2Request 构造走标准库 x/net http2.Transport(h2c) 的请求函数,
// 与 HttpClient.Request 等价逻辑,仅传输层不同,用于对照 H2FramerTransport。
func newStdH2Request(tb testing.TB, addr string) hrpc.Request {
	tb.Helper()
	std := &http2.Transport{
		AllowHTTP: true,
		DialTLSContext: func(ctx context.Context, network, address string, _ *tls.Config) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, address)
		},
	}
	client := &http.Client{Transport: std}
	tb.Cleanup(func() { std.CloseIdleConnections() })
	base := "http://" + addr + "/rpc/"
	return func(ctx context.Context, path string, notification bool, callback func(writer io.Writer) error) (io.ReadCloser, error) {
		buffer := bytes.NewBuffer(nil)
		if err := callback(buffer); err != nil {
			return nil, err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, buffer)
		if err != nil {
			return nil, err
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("unexpected status: %s", resp.Status)
		}
		return resp.Body, nil
	}
}

// concurrencyMatrix 运行并发矩阵:n 个 goroutine 各串行发 m 个请求,返回总耗时。
func concurrencyMatrix(tb testing.TB, n, m int, fn func() error) time.Duration {
	tb.Helper()
	start := make(chan struct{})
	errCh := make(chan error, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for j := 0; j < m; j++ {
				if err := fn(); err != nil {
					select {
					case errCh <- err:
					default:
					}
					return
				}
			}
		}()
	}
	begin := time.Now()
	close(start)
	wg.Wait()
	elapsed := time.Since(begin)
	select {
	case err := <-errCh:
		tb.Fatalf("matrix request failed: %v", err)
	default:
	}
	return elapsed
}

// wsConcurrencyMatrix 运行 WebSocket(WSS) 并发矩阵:33 连接分摊 n 个 goroutine,
// 每 goroutine 串行发 m 个请求,返回总耗时。与持续压测同连接数,动态端口自建服务。
func wsConcurrencyMatrix(tb testing.TB, n, m int) time.Duration {
	tb.Helper()
	rpcServer := hrpc.NewServer(hrpc.WithServerEncoder(hrpc.NewHBufEncode()), hrpc.WithServerDecode(hrpc.NewHBufDecode()))
	RegisterHbufService(rpcServer, &TestHbufService{})

	cert, err := tls.LoadX509KeyPair("/Users/dev/2.hbuf/hbuf_golang/pkg/hrpc/server.crt", "/Users/dev/2.hbuf/hbuf_golang/pkg/hrpc/server.key")
	if err != nil {
		tb.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		tb.Fatal(err)
	}
	wsServer := hrpc.NewWebSocketServer(rpcServer.Response, hrpc.WithWebSocketServerEncode(hrpc.NewHBufEncode()), hrpc.WithWebSocketServerDecode(hrpc.NewHBufDecode()))
	mux := http.NewServeMux()
	// 连接管理按 client id 区分,同 id 新连接会顶替旧连接,故每连接分配唯一 id
	var wsConnID atomic.Int64
	mux.HandleFunc("/socket", func(writer http.ResponseWriter, request *http.Request) {
		wsServer.ServeHTTP(writer, request, fmt.Sprintf("matrix-%d", wsConnID.Add(1)), nil, nil)
	})
	tlsListener := tls.NewListener(listener, &tls.Config{Certificates: []tls.Certificate{cert}})
	go func() { _ = http.Serve(tlsListener, mux) }()
	tb.Cleanup(func() { _ = tlsListener.Close() })

	const conns = 33
	start := make(chan struct{})
	errCh := make(chan error, n)
	var wg sync.WaitGroup
	for c := 0; c < conns; c++ {
		wsClient := hrpc.NewWebSocketClient("wss://"+listener.Addr().String()+"/socket", nil,
			hrpc.WithWebSocketClientInsecureSkipVerify(true),
			hrpc.WithWebSocketClientEncode(hrpc.NewHBufEncode()),
			hrpc.WithWebSocketClientDecode(hrpc.NewHBufDecode()))
		if err := wsClient.Connect(context.Background()); err != nil {
			tb.Fatal(err)
		}
		testClient := NewHbufServiceClient(hrpc.NewClient(wsClient.Request, hrpc.WithClientEncoder(hrpc.NewHBufEncode()), hrpc.WithClientDecode(hrpc.NewHBufDecode())))
		g := n / conns
		if c == conns-1 {
			g += n % conns
		}
		for i := 0; i < g; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				for j := 0; j < m; j++ {
					resp, err := testClient.HbufMethod(context.Background(), &HbufRequest{Name: "test", Age: 18})
					if err != nil {
						select {
						case errCh <- err:
						default:
						}
						return
					}
					if resp.Name != "test" || resp.Age != 18 {
						select {
						case errCh <- fmt.Errorf("resp mismatch: %+v", resp):
						default:
						}
						return
					}
				}
			}()
		}
	}
	begin := time.Now()
	close(start)
	wg.Wait()
	elapsed := time.Since(begin)
	select {
	case err := <-errCh:
		tb.Fatalf("wss matrix request failed: %v", err)
	default:
	}
	return elapsed
}

// Benchmark_H2Framer_VS_GRPC 对比 H2FramerTransport 与 gRPC 的串行调用性能。
// hrpc 侧使用 HBuf 编码、gRPC 侧使用 protobuf,均为各自最快路径。
func Benchmark_H2Framer_VS_GRPC(b *testing.B) {
	rpcServer := hrpc.NewServer(hrpc.WithServerEncoder(hrpc.NewHBufEncode()), hrpc.WithServerDecode(hrpc.NewHBufDecode()))
	RegisterHbufService(rpcServer, &TestHbufService{})
	addr := newH2cServer(b, rpcServer)

	transport := hrpc.NewH2FramerTransport()
	b.Cleanup(func() { transport.CloseIdleConnections() })
	client := hrpc.NewHttpClient("http://"+addr+"/rpc", hrpc.WithH2FramerTransport(transport))
	testClient := NewHbufServiceClient(hrpc.NewClient(client.Request, hrpc.WithClientEncoder(hrpc.NewHBufEncode()), hrpc.WithClientDecode(hrpc.NewHBufDecode())))

	b.Run("HRPC_H2Framer", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			resp, err := testClient.HbufMethod(context.Background(), &HbufRequest{Name: "test", Age: 18})
			if err != nil {
				b.Fatal(err)
			}
			if resp.Name != "test" || resp.Age != 18 {
				b.Fatal("test fail")
			}
		}
	})

	// 标准库 x/net http2.Transport 对照:同服务端同编解码链,仅传输层不同
	stdRequest := newStdH2Request(b, addr)
	stdTestClient := NewHbufServiceClient(hrpc.NewClient(stdRequest, hrpc.WithClientEncoder(hrpc.NewHBufEncode()), hrpc.WithClientDecode(hrpc.NewHBufDecode())))
	b.Run("HTTP2_StdTransport", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			resp, err := stdTestClient.HbufMethod(context.Background(), &HbufRequest{Name: "test", Age: 18})
			if err != nil {
				b.Fatal(err)
			}
			if resp.Name != "test" || resp.Age != 18 {
				b.Fatal("test fail")
			}
		}
	})

	grpcAddr := newGrpcServer(b)
	grpcClient, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = grpcClient.Close() })
	grpcTestClient := NewProtoServiceClient(grpcClient)
	if _, err := grpcTestClient.ProtoMethod(context.Background(), &ProtoRequest{Name: "test", Age: 18}); err != nil {
		b.Fatal(err)
	}

	b.Run("GRPC", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			resp, err := grpcTestClient.ProtoMethod(context.Background(), &ProtoRequest{Name: "test", Age: 18})
			if err != nil {
				b.Fatal(err)
			}
			if resp.Name != "test" || resp.Age != 18 {
				b.Fatal("test fail")
			}
		}
	})

	// 自研 H2FramerServer 服务端对照:与 x/net http2.Server 同编解码链,仅服务端传输不同
	framerServer := hrpc.NewServer(hrpc.WithServerEncoder(hrpc.NewHBufEncode()), hrpc.WithServerDecode(hrpc.NewHBufDecode()))
	RegisterHbufService(framerServer, &TestHbufService{})
	framerAddr := newH2FramerServer(b, framerServer)

	framerTransport := hrpc.NewH2FramerTransport()
	b.Cleanup(func() { framerTransport.CloseIdleConnections() })
	framerClient := hrpc.NewHttpClient("http://"+framerAddr+"/rpc", hrpc.WithH2FramerTransport(framerTransport))
	framerTestClient := NewHbufServiceClient(hrpc.NewClient(framerClient.Request, hrpc.WithClientEncoder(hrpc.NewHBufEncode()), hrpc.WithClientDecode(hrpc.NewHBufDecode())))
	b.Run("HRPC_H2Framer+FramerServer", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			resp, err := framerTestClient.HbufMethod(context.Background(), &HbufRequest{Name: "test", Age: 18})
			if err != nil {
				b.Fatal(err)
			}
			if resp.Name != "test" || resp.Age != 18 {
				b.Fatal("test fail")
			}
		}
	})

	framerStdRequest := newStdH2Request(b, framerAddr)
	framerStdClient := NewHbufServiceClient(hrpc.NewClient(framerStdRequest, hrpc.WithClientEncoder(hrpc.NewHBufEncode()), hrpc.WithClientDecode(hrpc.NewHBufDecode())))
	b.Run("HTTP2_StdTransport+FramerServer", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			resp, err := framerStdClient.HbufMethod(context.Background(), &HbufRequest{Name: "test", Age: 18})
			if err != nil {
				b.Fatal(err)
			}
			if resp.Name != "test" || resp.Age != 18 {
				b.Fatal("test fail")
			}
		}
	})
}

// Test_Concurrency_H2Framer_VS_GRPC 对比 H2FramerTransport 与 gRPC 的并发性能。
// 矩阵沿用既有惯例:1000 并发窗口 × 3 请求,双方均为单连接。
func Test_Concurrency_H2Framer_VS_GRPC(t *testing.T) {
	const goroutines, perGoroutine = 1000, 3

	rpcServer := hrpc.NewServer(hrpc.WithServerEncoder(hrpc.NewHBufEncode()), hrpc.WithServerDecode(hrpc.NewHBufDecode()))
	RegisterHbufService(rpcServer, &TestHbufService{})
	addr := newH2cServer(t, rpcServer)

	transport := hrpc.NewH2FramerTransport()
	defer transport.CloseIdleConnections()
	client := hrpc.NewHttpClient("http://"+addr+"/rpc", hrpc.WithH2FramerTransport(transport))
	testClient := NewHbufServiceClient(hrpc.NewClient(client.Request, hrpc.WithClientEncoder(hrpc.NewHBufEncode()), hrpc.WithClientDecode(hrpc.NewHBufDecode())))

	// 预热:建立连接
	if _, err := testClient.HbufMethod(context.Background(), &HbufRequest{Name: "test", Age: 18}); err != nil {
		t.Fatal(err)
	}
	hbufElapsed := concurrencyMatrix(t, goroutines, perGoroutine, func() error {
		resp, err := testClient.HbufMethod(context.Background(), &HbufRequest{Name: "test", Age: 18})
		if err != nil {
			return err
		}
		if resp.Name != "test" || resp.Age != 18 {
			return fmt.Errorf("resp mismatch: %+v", resp)
		}
		return nil
	})
	total := goroutines * perGoroutine
	t.Logf("HRPC_H2Framer: %d requests in %v, %.0f qps, avg %.1f µs/req",
		total, hbufElapsed, float64(total)/hbufElapsed.Seconds(), float64(hbufElapsed.Microseconds())/float64(total))

	// 标准库 x/net http2.Transport 对照:同服务端同编解码链,仅传输层不同
	stdRequest := newStdH2Request(t, addr)
	stdTestClient := NewHbufServiceClient(hrpc.NewClient(stdRequest, hrpc.WithClientEncoder(hrpc.NewHBufEncode()), hrpc.WithClientDecode(hrpc.NewHBufDecode())))
	if _, err := stdTestClient.HbufMethod(context.Background(), &HbufRequest{Name: "test", Age: 18}); err != nil {
		t.Fatal(err)
	}
	stdElapsed := concurrencyMatrix(t, goroutines, perGoroutine, func() error {
		resp, err := stdTestClient.HbufMethod(context.Background(), &HbufRequest{Name: "test", Age: 18})
		if err != nil {
			return err
		}
		if resp.Name != "test" || resp.Age != 18 {
			return fmt.Errorf("resp mismatch: %+v", resp)
		}
		return nil
	})
	t.Logf("HTTP2_StdTransport: %d requests in %v, %.0f qps, avg %.1f µs/req",
		total, stdElapsed, float64(total)/stdElapsed.Seconds(), float64(stdElapsed.Microseconds())/float64(total))

	grpcAddr := newGrpcServer(t)
	grpcClient, err := grpc.NewClient(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	defer grpcClient.Close()
	grpcTestClient := NewProtoServiceClient(grpcClient)

	// 预热:建立连接
	if _, err := grpcTestClient.ProtoMethod(context.Background(), &ProtoRequest{Name: "test", Age: 18}); err != nil {
		t.Fatal(err)
	}
	grpcElapsed := concurrencyMatrix(t, goroutines, perGoroutine, func() error {
		resp, err := grpcTestClient.ProtoMethod(context.Background(), &ProtoRequest{Name: "test", Age: 18})
		if err != nil {
			return err
		}
		if resp.Name != "test" || resp.Age != 18 {
			return fmt.Errorf("resp mismatch: %+v", resp)
		}
		return nil
	})
	t.Logf("GRPC: %d requests in %v, %.0f qps, avg %.1f µs/req",
		total, grpcElapsed, float64(total)/grpcElapsed.Seconds(), float64(grpcElapsed.Microseconds())/float64(total))

	// 自研 H2FramerServer 服务端对照:与 x/net http2.Server 同编解码链,仅服务端传输不同
	framerServer := hrpc.NewServer(hrpc.WithServerEncoder(hrpc.NewHBufEncode()), hrpc.WithServerDecode(hrpc.NewHBufDecode()))
	RegisterHbufService(framerServer, &TestHbufService{})
	framerAddr := newH2FramerServer(t, framerServer)

	framerTransport := hrpc.NewH2FramerTransport()
	defer framerTransport.CloseIdleConnections()
	framerClient := hrpc.NewHttpClient("http://"+framerAddr+"/rpc", hrpc.WithH2FramerTransport(framerTransport))
	framerTestClient := NewHbufServiceClient(hrpc.NewClient(framerClient.Request, hrpc.WithClientEncoder(hrpc.NewHBufEncode()), hrpc.WithClientDecode(hrpc.NewHBufDecode())))
	if _, err := framerTestClient.HbufMethod(context.Background(), &HbufRequest{Name: "test", Age: 18}); err != nil {
		t.Fatal(err)
	}
	framerElapsed := concurrencyMatrix(t, goroutines, perGoroutine, func() error {
		resp, err := framerTestClient.HbufMethod(context.Background(), &HbufRequest{Name: "test", Age: 18})
		if err != nil {
			return err
		}
		if resp.Name != "test" || resp.Age != 18 {
			return fmt.Errorf("resp mismatch: %+v", resp)
		}
		return nil
	})
	t.Logf("HRPC_H2Framer+FramerServer: %d requests in %v, %.0f qps, avg %.1f µs/req",
		total, framerElapsed, float64(total)/framerElapsed.Seconds(), float64(framerElapsed.Microseconds())/float64(total))

	// 标准库客户端与 H2FramerServer 互操作:隔离客户端差异,验证服务端批量合并收益
	framerStdRequest := newStdH2Request(t, framerAddr)
	framerStdClient := NewHbufServiceClient(hrpc.NewClient(framerStdRequest, hrpc.WithClientEncoder(hrpc.NewHBufEncode()), hrpc.WithClientDecode(hrpc.NewHBufDecode())))
	if _, err := framerStdClient.HbufMethod(context.Background(), &HbufRequest{Name: "test", Age: 18}); err != nil {
		t.Fatal(err)
	}
	framerStdElapsed := concurrencyMatrix(t, goroutines, perGoroutine, func() error {
		resp, err := framerStdClient.HbufMethod(context.Background(), &HbufRequest{Name: "test", Age: 18})
		if err != nil {
			return err
		}
		if resp.Name != "test" || resp.Age != 18 {
			return fmt.Errorf("resp mismatch: %+v", resp)
		}
		return nil
	})
	t.Logf("HTTP2_StdTransport+FramerServer: %d requests in %v, %.0f qps, avg %.1f µs/req",
		total, framerStdElapsed, float64(total)/framerStdElapsed.Seconds(), float64(framerStdElapsed.Microseconds())/float64(total))

	// WebSocket(WSS) 对照:33 连接分摊 1000 并发窗口,与持续压测同连接数
	wssElapsed := wsConcurrencyMatrix(t, goroutines, perGoroutine)
	t.Logf("HRPC_WSS: %d requests in %v, %.0f qps, avg %.1f µs/req",
		total, wssElapsed, float64(total)/wssElapsed.Seconds(), float64(wssElapsed.Microseconds())/float64(total))

	t.Logf("ratio: HRPC_H2Framer / StdTransport / HRPC+FramerServer / Std+FramerServer / HRPC_WSS / GRPC = %.2f / %.2f / %.2f / %.2f / %.2f / 1.00",
		float64(hbufElapsed)/float64(grpcElapsed), float64(stdElapsed)/float64(grpcElapsed),
		float64(framerElapsed)/float64(grpcElapsed), float64(framerStdElapsed)/float64(grpcElapsed),
		float64(wssElapsed)/float64(grpcElapsed))
}
