package hbuf_http

import (
	"context"
	hbuf "hbuf_golang/pkg"
	"io/ioutil"
	"net/http"
	"sync"
)

type HttpMapServer struct {
	router map[string]*hbuf.ServerInvoke
	lock   sync.RWMutex
}

func NewHttpServer() *HttpMapServer {
	return &HttpMapServer{}
}

func (h *HttpMapServer) AddServer(router hbuf.ServerRoute) {
	h.lock.Lock()
	defer h.lock.Unlock()
	for key, invokeMap := range router.InvokeMap() {
		h.router[router.Name()+"/"+key] = invokeMap
	}
}

func (h *HttpMapServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	h.lock.RLock()
	defer h.lock.RUnlock()
	route, ok := h.router[request.RequestURI]
	if !ok {
		writer.WriteHeader(http.StatusNotFound)
		return
	}
	buffer, err := ioutil.ReadAll(request.Body)
	if err != nil {
		return
	}
	form, err := route.Read(buffer)
	if err != nil {
		writer.WriteHeader(http.StatusForbidden)
		return
	}
	data, err := route.Call(context.TODO(), form)
	if err != nil {
		return
	}
	bytes, err := route.Writer(data)
	if err != nil {
		return
	}
	_, err = writer.Write(bytes)
	if err != nil {
		return
	}
}
