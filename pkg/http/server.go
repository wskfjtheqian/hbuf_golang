package http

import (
	"hbuf_golang/pkg/hbuf"
	"io/ioutil"
	ht "net/http"
	"sync"
)

type ErrorInterceptor = func(w ht.ResponseWriter, r *ht.Request, e error)
type readInvoke = func(w ht.ResponseWriter, r *ht.Request, buffer []byte, next ReadInterceptor) ([]byte, error)
type writerInvoke = func(w ht.ResponseWriter, r *ht.Request, buffer []byte, next WriterInterceptor) ([]byte, error)
type contextInvoke = func(w ht.ResponseWriter, r *ht.Request, ctx *hbuf.Context, data hbuf.Data, next ContextInterceptor) (*hbuf.Context, error)

type ContextInterceptor interface {
	Invoke() contextInvoke
	Next() ContextInterceptor
}

type contextInterceptorImp struct {
	invoke contextInvoke
	next   ContextInterceptor
}

func (c *contextInterceptorImp) Invoke() contextInvoke {
	return c.invoke
}

func (c *contextInterceptorImp) Next() ContextInterceptor {
	return c.next
}

type WriterInterceptor interface {
	Invoke() writerInvoke
	Next() WriterInterceptor
}

type writerInterceptorImp struct {
	invoke writerInvoke
	next   WriterInterceptor
}

func (c *writerInterceptorImp) Invoke() writerInvoke {
	return c.invoke
}

func (c *writerInterceptorImp) Next() WriterInterceptor {
	return c.next
}

type ReadInterceptor interface {
	Invoke() readInvoke
	Next() ReadInterceptor
}

type readInterceptorImp struct {
	invoke readInvoke
	next   ReadInterceptor
}

func (c *readInterceptorImp) Invoke() readInvoke {
	return c.invoke
}

func (c *readInterceptorImp) Next() ReadInterceptor {
	return c.next
}

type ServerJson struct {
	router     map[string]*hbuf.ServerInvoke
	lock       sync.RWMutex
	errorInc   ErrorInterceptor
	readInc    ReadInterceptor
	writerInc  WriterInterceptor
	contextInc ContextInterceptor
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
	ret.readInc = &readInterceptorImp{
		invoke: ret.readInterceptor,
	}
	ret.writerInc = &writerInterceptorImp{
		invoke: ret.writerInterceptor,
	}
	ret.contextInc = &contextInterceptorImp{
		invoke: ret.contextInterceptor,
	}
	return &ret
}

func (h *ServerJson) add(router hbuf.ServerRouter) {
	h.lock.Lock()
	defer h.lock.Unlock()

	for key, value := range router.GetInvoke() {
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

	buffer, err := h.readInc.Invoke()(w, r, []byte{}, h.readInc.Next())
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

	ctx, err := h.contextInc.Invoke()(w, r, &hbuf.Context{}, data, h.contextInc.Next())
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

	_, err = h.writerInc.Invoke()(w, r, buffer, h.writerInc.Next())
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

func (h *ServerJson) readInterceptor(w ht.ResponseWriter, r *ht.Request, buffer []byte, next ReadInterceptor) ([]byte, error) {
	buffer, err := ioutil.ReadAll(r.Body)
	if nil != err {
		return nil, NewHttpErrorByCode(ht.StatusInternalServerError)
	}
	if nil == next {
		return buffer, nil
	}
	ret, err := next.Invoke()(w, r, buffer, next.Next())
	if err != nil {
		return nil, err
	}
	if nil == ret {
		return buffer, nil
	}
	return ret, nil
}

func (h *ServerJson) writerInterceptor(w ht.ResponseWriter, r *ht.Request, buffer []byte, next WriterInterceptor) ([]byte, error) {
	if nil == next {
		_, err := w.Write(buffer)
		return nil, err
	}

	bytes, err := next.Invoke()(w, r, buffer, next.Next())
	if err != nil {
		return nil, err
	}
	_, err = w.Write(bytes)
	return nil, err
}

func (h *ServerJson) contextInterceptor(w ht.ResponseWriter, r *ht.Request, ctx *hbuf.Context, data hbuf.Data, next ContextInterceptor) (*hbuf.Context, error) {
	if nil == next {
		return ctx, nil
	}
	context, err := next.Invoke()(w, r, ctx, data, next.Next())
	if err != nil {
		return nil, err
	}
	if nil == context {
		return ctx, nil
	}
	return context, nil
}
