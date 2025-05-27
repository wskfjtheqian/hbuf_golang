package http

import (
	"errors"
	"fmt"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"github.com/wskfjtheqian/hbuf_golang/pkg/ip"
	"github.com/wskfjtheqian/hbuf_golang/pkg/utl"
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
		Handler: &a.mux,
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
	a.mux.ServeHTTP(writer, request)
	old = time.Now().UnixMilli() - old
	t := "[" + strconv.FormatFloat(float64(old)/1000, 'g', 3, 64) + "s]"
	if 200 > old {
		t = utl.Yellow(t)
	} else {
		t = utl.Red(t)
	}

	ip, _ := ip.GetHttpIP(request)
	value := reflect.ValueOf(writer).Elem()
	status := value.FieldByName("status")
	_ = hlog.Output(1, LogHTTP, fmt.Sprintln(t, ip, request.Method, request.Proto, status.Int(), utl.Green(request.URL.String())))
}
