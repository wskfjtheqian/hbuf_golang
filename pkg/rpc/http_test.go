package rpc_test

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
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"github.com/wskfjtheqian/hbuf_golang/pkg/rpc"
	"golang.org/x/net/http2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"io"
	"math/big"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type TestHbufService struct {
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
	rpcServer := rpc.NewServer()
	RegisterHbufService(rpcServer, &TestHbufService{})

	server := rpc.NewHttpServer("/rpc/", rpcServer)

	http.Handle("/rpc/", server)
	go http.ListenAndServe(":8080", nil)

	client := rpc.NewHttpClient("http://localhost:8080/rpc")

	rpcClient := rpc.NewClient(client.Request)
	testClient := NewHbufServiceClient(rpcClient)
	resp, err := testClient.HbufMethod(context.Background(), &HbufRequest{Name: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Name != "test" {
		t.Fatal("test fail")
	}
}

// 测试 HttpService 的 Response 方法
func TestHttpService_InvokeHBuf(t *testing.T) {
	rpcServer := rpc.NewServer(rpc.WithServerEncoder(rpc.NewHBufEncode()), rpc.WithServerDecode(rpc.NewHBufDecode()))
	RegisterHbufService(rpcServer, &TestHbufService{})

	server := rpc.NewHttpServer("/rpc/", rpcServer)

	http.Handle("/rpc/", server)
	go http.ListenAndServe(":8080", nil)

	client := rpc.NewHttpClient("http://localhost:8080/rpc")

	rpcClient := rpc.NewClient(client.Request, rpc.WithClientEncoder(rpc.NewHBufEncode()), rpc.WithClientDecode(rpc.NewHBufDecode()))
	testClient := NewHbufServiceClient(rpcClient)
	resp, err := testClient.HbufMethod(context.Background(), &HbufRequest{Name: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Name != "test" {
		t.Fatal("test fail")
	}
}

// 测试 HttpService 加密通信
func TestHttpService_Encoder(t *testing.T) {
	rpcServer := rpc.NewServer(rpc.WithServerEncoder(rpc.NewHBufEncode()), rpc.WithServerDecode(rpc.NewHBufDecode()))
	RegisterHbufService(rpcServer, &TestHbufService{})

	server := rpc.NewHttpServer("/rpc/", rpcServer, rpc.WithResponseMiddleware(func(next rpc.Response) rpc.Response {
		return func(ctx context.Context, path string, writer io.Writer, reader io.Reader) error {
			decoder := base64.NewDecoder(base64.StdEncoding, reader)

			encoder := base64.NewEncoder(base64.StdEncoding, writer)
			defer encoder.Close()

			return next(ctx, path, encoder, decoder)
		}
	}))

	http.Handle("/rpc/", server)
	go http.ListenAndServe(":8080", nil)

	client := rpc.NewHttpClient("http://localhost:8080/rpc", rpc.WithRequestMiddleware(func(next rpc.Request) rpc.Request {
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

	rpcClient := rpc.NewClient(client.Request, rpc.WithClientEncoder(rpc.NewHBufEncode()), rpc.WithClientDecode(rpc.NewHBufDecode()))
	testClient := NewHbufServiceClient(rpcClient)
	resp, err := testClient.HbufMethod(context.Background(), &HbufRequest{Name: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Name != "test" {
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
	rpcServer := rpc.NewServer(rpc.WithServerMiddleware(func(next rpc.Handler) rpc.Handler {
		return func(ctx context.Context, req hbuf.Data) (hbuf.Data, error) {
			return next(ctx, req)
		}
	}))
	RegisterHbufService(rpcServer, &TestHbufService{})

	server := rpc.NewHttpServer("/rpc/", rpcServer)

	http.Handle("/rpc/", server)
	go http.ListenAndServe(":8080", nil)

	client := rpc.NewHttpClient("http://localhost:8080/rpc")

	rpcClient := rpc.NewClient(client.Request)
	testClient := NewHbufServiceClient(rpcClient)
	resp, err := testClient.HbufMethod(context.Background(), &HbufRequest{Name: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Name != "test" {
		t.Fatal("test fail")
	}
}

func TestHttpService_Http2(t *testing.T) {
	rpcServer := rpc.NewServer()
	RegisterHbufService(rpcServer, &TestHbufService{})

	server := rpc.NewHttpServer("/rpc/", rpcServer)

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

	client := rpc.NewHttpClient("https://localhost:9080/rpc")

	rpcClient := rpc.NewClient(client.Request)
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

	rpcServer := rpc.NewServer()
	RegisterHbufService(rpcServer, &TestHbufService{})

	server := rpc.NewHttpServer("/rpc/", rpcServer)

	http.Handle("/rpc/", server)
	go func() {
		// 1. 生成私钥
		privateKey, err := generatePrivateKey()
		if err != nil {
			hlog.Error("generate private key failed: %s", err)
			return
		}

		// 5. 生成自签名证书
		cert, err := generateSelfSignedCert(privateKey)
		if err != nil {
			hlog.Error("generate self signed cert failed: %s", err)
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

	client := rpc.NewHttpClient("https://localhost:9080/rpc")

	rpcClient := rpc.NewClient(client.Request)
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
	rpcServer8 := rpc.NewServer(rpc.WithServerEncoder(rpc.NewHBufEncode()), rpc.WithServerDecode(rpc.NewHBufDecode()))
	RegisterHbufService(rpcServer8, &TestHbufService{})

	server8 := rpc.NewHttpServer("/rpc/", rpcServer8)

	mux8 := http.NewServeMux()
	mux8.Handle("/rpc/", server8)
	go http.ListenAndServe(":8180", mux8)

	client8 := rpc.NewHttpClient("http://localhost:8180/rpc")
	rpcClient8 := rpc.NewClient(client8.Request, rpc.WithClientEncoder(rpc.NewHBufEncode()), rpc.WithClientDecode(rpc.NewHBufDecode()))
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

	rpcServer := rpc.NewServer()
	RegisterHbufService(rpcServer, &TestHbufService{})

	server := rpc.NewHttpServer("/rpc/", rpcServer)

	mux := http.NewServeMux()
	mux.Handle("/rpc/", server)
	go http.ListenAndServe(":8080", mux)

	client := rpc.NewHttpClient("http://localhost:8080/rpc")
	rpcClient := rpc.NewClient(client.Request)
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

	rpcServer1 := rpc.NewServer()
	RegisterHbufService(rpcServer1, &TestHbufService{})

	server1 := rpc.NewHttpServer("/rpc/", rpcServer1)

	mux1 := http.NewServeMux()
	mux1.Handle("/rpc/", server1)
	go http.ListenAndServeTLS(":8081", "E:\\develop\\hbuf\\hbuf_golang\\pkg\\rpc\\server.crt", "E:\\develop\\hbuf\\hbuf_golang\\pkg\\rpc\\server.key", mux1)

	client1 := rpc.NewHttpClient("https://localhost:8081/rpc")
	rpcClient1 := rpc.NewClient(client1.Request)
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

	rpcServer2 := rpc.NewServer()
	RegisterHbufService(rpcServer2, &TestHbufService{})

	server2 := rpc.NewWebSocketServer(rpcServer2.Response)

	mux2 := http.NewServeMux()
	mux2.Handle("/socket", server2)
	go http.ListenAndServe(":8084", mux2)

	client2 := rpc.NewWebSocketClient("ws://localhost:8084/socket", nil)

	err := client2.Connect(context.Background())
	if err != nil {
		b.Fatal(err)
	}
	rpcClient2 := rpc.NewClient(client2.Request)
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

	rpcServer3 := rpc.NewServer()
	RegisterHbufService(rpcServer3, &TestHbufService{})

	server3 := rpc.NewWebSocketServer(rpcServer3.Response)

	mux3 := http.NewServeMux()
	mux3.Handle("/socket", server3)
	go http.ListenAndServeTLS(":8085", "E:\\develop\\hbuf\\hbuf_golang\\pkg\\rpc\\server.crt", "E:\\develop\\hbuf\\hbuf_golang\\pkg\\rpc\\server.key", mux3)

	client3 := rpc.NewWebSocketClient("wss://localhost:8085/socket", nil)

	err = client3.Connect(context.Background())
	if err != nil {
		b.Fatal(err)
	}
	rpcClient3 := rpc.NewClient(client3.Request)
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

	go http.ListenAndServeTLS(":8083", "E:\\develop\\hbuf\\hbuf_golang\\pkg\\rpc\\server.crt", "E:\\develop\\hbuf\\hbuf_golang\\pkg\\rpc\\server.key", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

	timeLength := 10
	end := now.Load() + int64(time.Duration(timeLength)*time.Second/time.Millisecond)

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
	hRpcServer := rpc.NewServer(rpc.WithServerEncoder(rpc.NewHBufEncode()), rpc.WithServerDecode(rpc.NewHBufDecode()))
	RegisterHbufService(hRpcServer, hrpcService)
	mux := http.NewServeMux()
	mux.Handle("/rpc/", rpc.NewHttpServer("/rpc/", hRpcServer))
	go http.ListenAndServeTLS(":8081", "E:\\develop\\hbuf\\hbuf_golang\\pkg\\rpc\\server.crt", "E:\\develop\\hbuf\\hbuf_golang\\pkg\\rpc\\server.key", mux)

	httpClient := rpc.NewHttpClient("https://localhost:8081/rpc")
	rpcClient := rpc.NewClient(httpClient.Request, rpc.WithClientEncoder(rpc.NewHBufEncode()), rpc.WithClientDecode(rpc.NewHBufDecode()))
	rpcTestClient := NewHbufServiceClient(rpcClient)

	_, err = rpcTestClient.HbufMethod(context.Background(), &HbufRequest{Name: "test " + strconv.Itoa(-1)})
	if err != nil {
		t.Error(err)
		return
	}

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

	func() {
		// 测试 HRPC_WS 并发性能
		wsService := &HbufServiceConcurrency{}
		rpcServer3 := rpc.NewServer(rpc.WithServerEncoder(rpc.NewHBufEncode()), rpc.WithServerDecode(rpc.NewHBufDecode()))
		RegisterHbufService(rpcServer3, wsService)

		wsServer := rpc.NewWebSocketServer(rpcServer3.Response)

		mux := http.NewServeMux()
		mux.Handle("/socket", wsServer)
		go http.ListenAndServeTLS(":8082", "E:\\develop\\hbuf\\hbuf_golang\\pkg\\rpc\\server.crt", "E:\\develop\\hbuf\\hbuf_golang\\pkg\\rpc\\server.key", mux)

		timeLength := 10
		var wg sync.WaitGroup
		for i := 0; i < 33; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				wsClient := rpc.NewWebSocketClient("wss://localhost:8082/socket", nil)
				wsRpcClient := rpc.NewClient(wsClient.Request, rpc.WithClientEncoder(rpc.NewHBufEncode()), rpc.WithClientDecode(rpc.NewHBufDecode()))
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
	}()

}
