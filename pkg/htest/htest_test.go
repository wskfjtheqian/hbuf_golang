package htest_test

import (
	"context"
	"log"
	"net/http"
	"testing"
	"time"

	"github.com/wskfjtheqian/hbuf_golang/pkg/htest"
)

// ApiHttpRequest
func ApiHttpRequest(t *htest.T) {
	t.Call(func() (any, error) {
		get, err := http.Get("http://localhost:20001/api/request")
		if err != nil {
			return nil, err
		}
		get.Body.Close()
		return nil, nil
	}())
}

func TestEngine_Main(t *testing.T) {
	go func() {
		http.HandleFunc("/api/request", func(w http.ResponseWriter, r *http.Request) {
			return
		})
		err := http.ListenAndServe(":20001", nil)
		if err != nil {
			t.Error(err)
			return
		}
	}()
	<-time.After(time.Second * 2)

	engine := htest.NewEngine()
	engine.WithContext = func(ctx context.Context) context.Context {
		return ctx
	}
	engine.AddApi("空请求", 50, ApiHttpRequest)

	// 启动Web服务器
	webServer := htest.NewServer(engine, ":20002")

	log.Println("压力测试服务启动，访问 http://localhost:20002")
	log.Fatal(webServer.Start())
}
