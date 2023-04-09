package http

//
//import (
//	"context"
//	"encoding/json"
//	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
//	utl "github.com/wskfjtheqian/hbuf_golang/pkg/utils"
//	"io/ioutil"
//	"log"
//	ht "net/http"
//	"reflect"
//	"sync"
//)
//
//type ErrorFilter = func(w ht.ResponseWriter, r *ht.Request, e error) *hbuf.Result
//type ReadFilter = func(w ht.ResponseWriter, r *ht.Request, buffer []byte) (ht.ResponseWriter, *ht.Request, []byte, error)
//type WriterFilter = func(w ht.ResponseWriter, r *ht.Request, buffer []byte) (ht.ResponseWriter, *ht.Request, []byte, error)
//
//type Error struct {
//	Code int
//}
//
//func (e *Error) Error() string {
//	return ht.StatusText(e.Code)
//}
//
//type serverJsonContext struct {
//	context.Context
//	value *ServerJsonContextValue
//}
//
//type ServerJsonContextValue struct {
//	Writer  ht.ResponseWriter
//	Request *ht.Request
//}
//
//var payType = reflect.TypeOf(&serverJsonContext{})
//
//func (d *serverJsonContext) Value(key any) any {
//	if reflect.TypeOf(d) == key {
//		return d.value
//	}
//	return d.Context.Value(key)
//}
//func (d *serverJsonContext) Done() <-chan struct{} {
//	return d.Context.Done()
//}
//func Get(ctx context.Context) *ServerJsonContextValue {
//	ret := ctx.Value(payType)
//	if nil == ret {
//		return nil
//	}
//	return ret.(*ServerJsonContextValue)
//}
//
//type ServerJson struct {
//	server       *hbuf.Server
//	lock         sync.RWMutex
//	errorFilter  ErrorFilter
//	readFilter   []ReadFilter
//	writerFilter []WriterFilter
//	pathPrefix   string
//}
//
//func (s *ServerJson) SetErrorFilter(filter ErrorFilter) {
//	if nil == filter {
//		return
//	}
//	s.errorFilter = filter
//}
//
//func NewServerJson(server *hbuf.Server, pathPrefix string) *ServerJson {
//	ret := &ServerJson{
//		server:       server,
//		readFilter:   []ReadFilter{},
//		writerFilter: []WriterFilter{},
//		pathPrefix:   pathPrefix,
//	}
//	return ret
//}
//
//func (s *ServerJson) AddReadFilter(inc ReadFilter) {
//	s.lock.Lock()
//	defer s.lock.Unlock()
//	s.readFilter = append(s.readFilter, inc)
//}
//
//func (s *ServerJson) InsertReadFilter(inc ReadFilter) {
//	s.lock.Lock()
//	defer s.lock.Unlock()
//	s.readFilter = append([]ReadFilter{inc}, s.readFilter...)
//}
//
//func (s *ServerJson) AddWriterFilter(inc WriterFilter) {
//	s.lock.Lock()
//	defer s.lock.Unlock()
//	s.writerFilter = append(s.writerFilter, inc)
//}
//
//func (s *ServerJson) InsertWriterFilter(inc WriterFilter) {
//	s.lock.Lock()
//	defer s.lock.Unlock()
//	s.writerFilter = append([]WriterFilter{inc}, s.writerFilter...)
//}
//
//func (s *ServerJson) ServeHTTP(w ht.ResponseWriter, r *ht.Request) {
//	s.lock.RLock()
//	defer s.lock.RUnlock()
//
//	value, ok := s.server.Router()[r.URL.Path[len(s.pathPrefix):]]
//	if !ok {
//		s.onErrorFilter(w, r, &Error{Code: ht.StatusNotFound})
//		return
//	}
//
//	_, _, buffer, erro := s.onReadFilter(w, r, []byte{})
//	if nil != erro {
//		s.onErrorFilter(w, r, erro)
//		return
//	}
//
//	data, erro := value.ToData(buffer)
//	if nil != erro {
//		s.onErrorFilter(w, r, erro)
//		return
//	}
//	contX := &serverJsonContext{
//		Context: hbuf.NewContext(context.TODO()),
//		value: &ServerJsonContextValue{
//			Writer:  w,
//			Request: r,
//		},
//	}
//	defer hbuf.CloseContext(contX)
//
//	for key, _ := range r.Header {
//		hbuf.SetHeader(contX, key, r.Header.Get(key))
//	}
//	value.SetInfo(contX)
//	hbuf.SetMethod(contX, r.URL.Path)
//
//	ctx, erro := s.server.Filter(contX)
//	if nil != erro {
//		s.onErrorFilter(w, r, erro)
//		return
//	}
//
//	hbuf.SetContextOnClone(ctx, func(ctx context.Context) (context.Context, error) {
//		c, erro := s.server.Filter(ctx)
//		if erro != nil {
//			return nil, erro
//		}
//		return c, nil
//	})
//
//	data, erro = value.Invoke(ctx, data)
//	if nil != erro {
//		s.onErrorFilter(w, r, erro)
//		return
//	}
//
//	buffer, erro = value.FormData(data)
//	if nil != erro {
//		s.onErrorFilter(w, r, erro)
//		return
//	}
//
//	ret := &hbuf.Result{
//		Data: buffer,
//	}
//	buffer, erro = json.Marshal(ret)
//	_, _, _, _ = s.onWriterResult(w, r, buffer)
//}
//
//func (s *ServerJson) onReadFilter(w ht.ResponseWriter, r *ht.Request, buffer []byte) (ht.ResponseWriter, *ht.Request, []byte, error) {
//	buffer, erro := ioutil.ReadAll(r.Body)
//	if nil != erro {
//		return nil, nil, nil, &Error{Code: ht.StatusInternalServerError}
//	}
//
//	for _, filter := range s.readFilter {
//		w, r, buffer, erro = filter(w, r, buffer)
//		if erro != nil {
//			return nil, nil, nil, erro
//		}
//	}
//	return w, r, buffer, nil
//}
//
//func (s *ServerJson) onErrorFilter(w ht.ResponseWriter, r *ht.Request, e error) {
//	if nil != s.errorFilter {
//		e = s.errorFilter(w, r, e)
//	}
//	if nil == e {
//		return
//	}
//	switch e.(type) {
//	case *hbuf.Result:
//		buffer, erro := json.Marshal(e.(*hbuf.Result))
//		if erro != nil {
//			w.WriteHeader(ht.StatusInternalServerError)
//			log.Println("Error:", e.Error()+"----"+r.URL.String())
//			return
//		}
//		_, _, _, _ = s.onWriterResult(w, r, buffer)
//		return
//	case *utl.Error:
//		e.(*utl.Error).PrintStack()
//	case *Error:
//		log.Println("Error:", e.Error()+"----"+r.URL.String())
//		switch e.(*Error).Code {
//		case ht.StatusNotFound, ht.StatusInternalServerError:
//			w.WriteHeader(e.(*Error).Code)
//			return
//		}
//	default:
//		log.Println("Error:", e.Error()+"----"+r.URL.String())
//	}
//	w.WriteHeader(ht.StatusInternalServerError)
//	return
//}
//
//func (s *ServerJson) onWriterResult(w ht.ResponseWriter, r *ht.Request, buffer []byte) (ht.ResponseWriter, *ht.Request, []byte, error) {
//	var erro error
//	for _, filter := range s.writerFilter {
//		w, r, buffer, erro = filter(w, r, buffer)
//		if erro != nil {
//			return nil, nil, nil, erro
//		}
//	}
//	_, erro = w.Write(buffer)
//	if erro != nil {
//		w.WriteHeader(ht.StatusInternalServerError)
//		println("ResponseWriter Error: %s", erro.Error())
//		return nil, nil, nil, erro
//	}
//	return nil, nil, nil, nil
//}
