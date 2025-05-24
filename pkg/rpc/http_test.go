package rpc

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
	hbuf "github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"io"
	"math/big"
	"net/http"
	"sync"
	"testing"
	"time"
)

// 测试 HttpService 的 Response 方法
func TestHttpService_Invoke(t *testing.T) {
	rpcServer := NewServer()
	RegisterRpcServer(rpcServer, &TestRpcServer{})

	server := NewHttpServer("/rpc/", rpcServer)

	http.Handle("/rpc/", server)
	go http.ListenAndServe(":8080", nil)

	client := NewHttpClient("http://localhost:8080/rpc")

	rpcClient := NewClient(client.Request)
	testClient := NewTestRpcClient(rpcClient)
	resp, err := testClient.GetName(context.Background(), &GetNameRequest{Name: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Name != "test" {
		t.Fatal("test fail")
	}
}

// 测试 HttpService 的 Response 方法
func TestHttpService_InvokeHBuf(t *testing.T) {
	rpcServer := NewServer(WithServerEncoder(NewHBufEncode()), WithServerDecode(NewHBufDecode()))
	RegisterRpcServer(rpcServer, &TestRpcServer{})

	server := NewHttpServer("/rpc/", rpcServer)

	http.Handle("/rpc/", server)
	go http.ListenAndServe(":8080", nil)

	client := NewHttpClient("http://localhost:8080/rpc")

	rpcClient := NewClient(client.Request, WithClientEncoder(NewHBufEncode()), WithClientDecode(NewHBufDecode()))
	testClient := NewTestRpcClient(rpcClient)
	resp, err := testClient.GetName(context.Background(), &GetNameRequest{Name: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Name != "test" {
		t.Fatal("test fail")
	}
}

// 测试 HttpService 加密通信
func TestHttpService_Encoder(t *testing.T) {
	rpcServer := NewServer(WithServerEncoder(NewHBufEncode()), WithServerDecode(NewHBufDecode()))
	RegisterRpcServer(rpcServer, &TestRpcServer{})

	server := NewHttpServer("/rpc/", rpcServer, WithResponseMiddleware(func(next Response) Response {
		return func(ctx context.Context, path string, writer io.Writer, reader io.Reader) error {
			decoder := base64.NewDecoder(base64.StdEncoding, reader)

			encoder := base64.NewEncoder(base64.StdEncoding, writer)
			defer encoder.Close()

			return next(ctx, path, encoder, decoder)
		}
	}))

	http.Handle("/rpc/", server)
	go http.ListenAndServe(":8080", nil)

	client := NewHttpClient("http://localhost:8080/rpc", WithRequestMiddleware(func(next Request) Request {
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

	rpcClient := NewClient(client.Request, WithClientEncoder(NewHBufEncode()), WithClientDecode(NewHBufDecode()))
	testClient := NewTestRpcClient(rpcClient)
	resp, err := testClient.GetName(context.Background(), &GetNameRequest{Name: "test"})
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
	rpcServer := NewServer(WithServerMiddleware(func(next Handler) Handler {
		return func(ctx context.Context, req hbuf.Data) (hbuf.Data, error) {
			return next(ctx, req)
		}
	}))
	RegisterRpcServer(rpcServer, &TestRpcServer{})

	server := NewHttpServer("/rpc/", rpcServer)

	http.Handle("/rpc/", server)
	go http.ListenAndServe(":8080", nil)

	client := NewHttpClient("http://localhost:8080/rpc")

	rpcClient := NewClient(client.Request)
	testClient := NewTestRpcClient(rpcClient)
	resp, err := testClient.GetName(context.Background(), &GetNameRequest{Name: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Name != "test" {
		t.Fatal("test fail")
	}
}

func TestHttpService_Http2(t *testing.T) {
	rpcServer := NewServer()
	RegisterRpcServer(rpcServer, &TestRpcServer{})

	server := NewHttpServer("/rpc/", rpcServer)

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

	client := NewHttpClient("https://localhost:9080/rpc")

	rpcClient := NewClient(client.Request)
	testClient := NewTestRpcClient(rpcClient)
	<-time.After(time.Second * 5)

	await := sync.WaitGroup{}
	for i := 0; i < 2; i++ {
		await.Add(1)
		go func() {
			resp, err := testClient.GetName(context.Background(), &GetNameRequest{Name: "test"})
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

	rpcServer := NewServer()
	RegisterRpcServer(rpcServer, &TestRpcServer{})

	server := NewHttpServer("/rpc/", rpcServer)

	http.Handle("/rpc/", server)
	go func() {
		// 1. 生成私钥
		privateKey, err := generatePrivateKey()
		if err != nil {
			hlog.Error("generate private key failed: ", err)
			return
		}

		// 5. 生成自签名证书
		cert, err := generateSelfSignedCert(privateKey)
		if err != nil {
			hlog.Error("generate self signed cert failed: ", err)
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

	client := NewHttpClient("https://localhost:9080/rpc")

	rpcClient := NewClient(client.Request)
	testClient := NewTestRpcClient(rpcClient)
	resp, err := testClient.GetName(context.Background(), &GetNameRequest{Name: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Name != "test" {
		t.Fatal("test fail")
	}
}
