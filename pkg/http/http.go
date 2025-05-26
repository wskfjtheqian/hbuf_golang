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
	mux    *http.ServeMux
	http   *http.Server
	config Config

	init     chan bool
	isInit   bool
	listener net.Listener
}

func NewHttp() *Http {
	return &Http{
		init: make(chan bool, 1),
	}
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

	old := a.http
	defer func() {
		if old != nil {
			<-time.After(time.Second * 30)
			_ = old.Close()
			hlog.Info("close old http connection")
		}
	}()

	oldListener := a.listener
	if nil == conf {
		if nil != oldListener {
			_ = oldListener.Close()
		}
		a.listener = nil
		a.http = nil
		return nil
	}

	a.mux = &http.ServeMux{}
	a.mux.HandleFunc("/health", a.health)

	addr := *conf.Addr
	if !utl.Equal(a.config.Addr, &addr) {
		var err error
		a.listener, err = net.Listen("tcp", addr)
		if err != nil {
			hlog.Error("Listen server failed with '%s'\n", err)
			return nil
		}

		go func() {
			if oldListener != nil {
				<-time.After(time.Second * 2)
				_ = oldListener.Close()
				hlog.Info("close old tcp listener: %s", addr)
			}
		}()
	}

	a.http = &http.Server{
		Handler: a.mux,
	}

	go func() {
		if !a.isInit {
			<-a.init
		}
		var err error
		if conf.Crt != nil && conf.Key != nil {
			hlog.Info("Start https server, addr: %s", addr)
			err = a.http.ServeTLS(a.listener, *conf.Crt, *conf.Key)
		} else {
			hlog.Info("Start http server, addr: %s", addr)
			err = a.http.Serve(a.listener)
		}
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				hlog.Info("Server closed %s", addr)
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
