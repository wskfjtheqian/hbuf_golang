package http

import (
	"hbuf_golang/pkg/hbuf"
	"io/ioutil"
	ht "net/http"
	"sync"
)

type ErrorInterceptor = func(w ht.ResponseWriter, r *ht.Request, e error)
type ReadInterceptor = func(w ht.ResponseWriter, r *ht.Request) ([]byte, error)
type WriterInterceptor = func(w ht.ResponseWriter, r *ht.Request, buffer []byte) error
type Interceptor = func(w ht.ResponseWriter, r *ht.Request, data hbuf.Data) (*hbuf.Context, error)

type ServerJson struct {
	router     map[string]*hbuf.ServerInvoke
	lock       sync.RWMutex
	errorInc   ErrorInterceptor
	readInc    ReadInterceptor
	writerInc  WriterInterceptor
	contextInc Interceptor
}

func (h *ServerJson) SetErrorInterceptor(inc ErrorInterceptor) {
	if nil == inc {
		return
	}

	h.errorInc = inc
}

func NewServerJson() *ServerJson {
	ret := ServerJson{
		router: map[string]*hbuf.ServerInvoke{},
	}
	ret.errorInc = ret.errorInterceptor
	ret.readInc = ret.readInterceptor
	ret.writerInc = ret.writerInterceptor
	ret.contextInc = ret.contextInterceptor

	return &ret
}

func (h *ServerJson) add(router hbuf.ServerRoute) {
	h.lock.Lock()
	defer h.lock.Unlock()

	for key, value := range router.GetIdInvoke() {
		h.router["/"+router.GetName()+"/"+key] = value
	}
}

func (h *ServerJson) ServeHTTP(w ht.ResponseWriter, r *ht.Request) {
	h.lock.RLock()
	defer h.lock.RUnlock()

	value, ok := h.router[r.URL.Path]
	if !ok {
		h.errorInc(w, r, NewHttpErrorByCode(ht.StatusNotFound))
		return
	}

	buffer, err := h.readInc(w, r)
	if nil != err {
		println("ReadInterceptor Error: %s", err.Error())
		h.errorInc(w, r, NewHttpErrorByCode(ht.StatusInternalServerError))
		return
	}

	data, err := value.ToData(buffer)
	if nil != err {
		println("ToData Error: %s", err.Error())
		h.errorInc(w, r, NewHttpErrorByCode(ht.StatusInternalServerError))
		return
	}

	ctx, err := h.contextInc(w, r, data)
	if nil != err {
		h.errorInc(w, r, err)
		return
	}

	data, err = value.Invoke(ctx, data)
	if nil != err {
		h.errorInc(w, r, err)
		return
	}

	buffer, err = value.FormData(data)
	if nil != err {
		println("FormData Error: %s", err.Error())
		h.errorInc(w, r, NewHttpErrorByCode(ht.StatusInternalServerError))
		return
	}

	err = h.writerInc(w, r, buffer)
	if nil != err {
		println("ResponseWriter Error: %s", err.Error())
	}
}

func (h *ServerJson) errorInterceptor(w ht.ResponseWriter, r *ht.Request, e error) {
	switch e.(type) {
	case *HttpError:
		w.WriteHeader(e.(*HttpError).code)
		return
	}

}

func (h *ServerJson) readInterceptor(w ht.ResponseWriter, r *ht.Request) ([]byte, error) {
	buffer, err := ioutil.ReadAll(r.Body)
	if nil != err {
		return nil, NewHttpErrorByCode(ht.StatusInternalServerError)
	}
	return buffer, nil
}

func (h *ServerJson) writerInterceptor(w ht.ResponseWriter, r *ht.Request, buffer []byte) error {
	_, err := w.Write(buffer)
	return err
}

func (h *ServerJson) contextInterceptor(w ht.ResponseWriter, r *ht.Request, data hbuf.Data) (*hbuf.Context, error) {
	return &hbuf.Context{}, nil
}
