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
	next   *contextInterceptorImp
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
	next   *writerInterceptorImp
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
	next   *readInterceptorImp
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
	readInc    *readInterceptorImp
	writerInc  *writerInterceptorImp
	contextInc *contextInterceptorImp
}

func (s *ServerJson) SetErrorInterceptor(inc ErrorInterceptor) {
	if nil == inc {
		return
	}

	s.errorInc = inc
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

func (s *ServerJson) addRequestInterceptor(inc readInvoke) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.readInc = &readInterceptorImp{
		invoke: inc,
		next:   s.readInc,
	}
}

func (s *ServerJson) insertRequestInterceptor(inc readInvoke) {
	s.lock.Lock()
	defer s.lock.Unlock()
	temp := s.readInc
	for nil != temp {
		temp = temp.next
	}
	temp.next = &readInterceptorImp{
		invoke: inc,
	}
}

func (s *ServerJson) addResponseInterceptor(inc writerInvoke) {
	s.lock.Lock()
	defer s.lock.Unlock()
	temp := s.writerInc
	for nil != temp {
		temp = temp.next
	}
	temp.next = &writerInterceptorImp{
		invoke: inc,
	}
}

func (s *ServerJson) insertResponseInterceptor(inc writerInvoke) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.writerInc = &writerInterceptorImp{
		invoke: inc,
		next:   s.writerInc,
	}
}

func (s *ServerJson) addContextInterceptor(inc contextInvoke) {
	s.lock.Lock()
	defer s.lock.Unlock()
	temp := s.contextInc
	for nil != temp {
		temp = temp.next
	}
	temp.next = &contextInterceptorImp{
		invoke: inc,
	}
}

func (s *ServerJson) insertContextInterceptor(inc contextInvoke) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.contextInc = &contextInterceptorImp{
		invoke: inc,
		next:   s.contextInc,
	}
}

func (s *ServerJson) add(router hbuf.ServerRouter) {
	s.lock.Lock()
	defer s.lock.Unlock()

	for key, value := range router.GetInvoke() {
		s.router["/"+router.GetName()+"/"+key] = value
	}
}

func (s *ServerJson) ServeHTTP(w ht.ResponseWriter, r *ht.Request) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	value, ok := s.router[r.URL.Path]
	if !ok {
		s.errorInc(w, r, NewHttpErrorByCode(ht.StatusNotFound))
		return
	}

	buffer, err := s.readInc.Invoke()(w, r, []byte{}, s.readInc.Next())
	if nil != err {
		println("ReadInterceptor Error: %s", err.Error())
		s.errorInc(w, r, NewHttpErrorByCode(ht.StatusInternalServerError))
		return
	}

	data, err := value.ToData(buffer)
	if nil != err {
		println("ToData Error: %s", err.Error())
		s.errorInc(w, r, NewHttpErrorByCode(ht.StatusInternalServerError))
		return
	}

	ctx, err := s.contextInc.Invoke()(w, r, &hbuf.Context{}, data, s.contextInc.Next())
	if nil != err {
		s.errorInc(w, r, err)
		return
	}

	data, err = value.Invoke(ctx, data)
	if nil != err {
		s.errorInc(w, r, err)
		return
	}

	buffer, err = value.FormData(data)
	if nil != err {
		println("FormData Error: %s", err.Error())
		s.errorInc(w, r, NewHttpErrorByCode(ht.StatusInternalServerError))
		return
	}

	_, err = s.writerInc.Invoke()(w, r, buffer, s.writerInc.Next())
	if nil != err {
		println("ResponseWriter Error: %s", err.Error())
	}
}

func (s *ServerJson) errorInterceptor(w ht.ResponseWriter, r *ht.Request, e error) {
	switch e.(type) {
	case *HttpError:
		w.WriteHeader(e.(*HttpError).code)
		return
	}
}

func (s *ServerJson) readInterceptor(w ht.ResponseWriter, r *ht.Request, buffer []byte, next ReadInterceptor) ([]byte, error) {
	buffer, err := ioutil.ReadAll(r.Body)
	if nil != err {
		return nil, NewHttpErrorByCode(ht.StatusInternalServerError)
	}
	if IsNil(next) {
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

func (s *ServerJson) writerInterceptor(w ht.ResponseWriter, r *ht.Request, buffer []byte, next WriterInterceptor) ([]byte, error) {
	if IsNil(next) {
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

func (s *ServerJson) contextInterceptor(w ht.ResponseWriter, r *ht.Request, ctx *hbuf.Context, data hbuf.Data, next ContextInterceptor) (*hbuf.Context, error) {
	if IsNil(next) {
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
