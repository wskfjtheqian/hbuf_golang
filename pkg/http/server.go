package http

import (
	"context"
	"encoding/json"
	"hbuf_golang/pkg/hbuf"
	"io/ioutil"
	ht "net/http"
	"sync"
)

type ErrorFilter = func(w ht.ResponseWriter, r *ht.Request, e error) *hbuf.Result
type ReadFilter = func(w ht.ResponseWriter, r *ht.Request, buffer []byte) (ht.ResponseWriter, *ht.Request, []byte, error)
type WriterFilter = func(w ht.ResponseWriter, r *ht.Request, buffer []byte) (ht.ResponseWriter, *ht.Request, []byte, error)

type Error struct {
	Code int
}

func (e *Error) Error() string {
	return ht.StatusText(e.Code)
}

type ServerJson struct {
	server       *hbuf.Server
	lock         sync.RWMutex
	errorFilter  ErrorFilter
	readFilter   []ReadFilter
	writerFilter []WriterFilter
}

func (s *ServerJson) SetErrorFilter(filter ErrorFilter) {
	if nil == filter {
		return
	}
	s.errorFilter = filter
}

func NewServerJson(server *hbuf.Server) *ServerJson {
	ret := &ServerJson{
		server:       server,
		readFilter:   []ReadFilter{},
		writerFilter: []WriterFilter{},
	}
	return ret
}

func (s *ServerJson) AddReadFilter(inc ReadFilter) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.readFilter = append(s.readFilter, inc)
}

func (s *ServerJson) InsertReadFilter(inc ReadFilter) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.readFilter = append([]ReadFilter{inc}, s.readFilter...)
}

func (s *ServerJson) AddWriterFilter(inc WriterFilter) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.writerFilter = append(s.writerFilter, inc)
}

func (s *ServerJson) InsertWriterFilter(inc WriterFilter) {
	s.lock.Lock()
	defer s.lock.Unlock()
	s.writerFilter = append([]WriterFilter{inc}, s.writerFilter...)
}

func (s *ServerJson) ServeHTTP(w ht.ResponseWriter, r *ht.Request) {
	s.lock.RLock()
	defer s.lock.RUnlock()

	value, ok := s.server.Router()[r.URL.Path]
	if !ok {
		ret := s.onErrorFilter(w, r, &Error{Code: ht.StatusNotFound})
		if nil != ret {
			return
		}
	}

	_, _, buffer, err := s.onReadFilter(w, r, []byte{})
	if nil != err {
		ret := s.onErrorFilter(w, r, err)
		if nil != ret {
			return
		}
		return
	}

	data, err := value.ToData(buffer)
	if nil != err {
		ret := s.onErrorFilter(w, r, &Error{Code: ht.StatusInternalServerError})
		if nil != ret {
			return
		}
	}

	contX := hbuf.NewContext(context.Background())
	for key, _ := range r.Header {
		hbuf.SetHeader(contX, key, r.Header.Get(key))
	}

	ctx, err := s.server.Filter(contX)
	if nil != err {
		ret := s.onErrorFilter(w, r, err)
		if nil != ret {
			return
		}
	}

	data, err = value.Invoke(ctx, data)
	if nil != err {
		ret := s.onErrorFilter(w, r, err)
		if nil != ret {
			return
		}
	}

	buffer, err = value.FormData(data)
	if nil != err {
		ret := s.onErrorFilter(w, r, &Error{Code: ht.StatusInternalServerError})
		if nil != ret {
			return
		}
	}

	ret := &hbuf.Result{
		Data: string(buffer),
	}
	buffer, err = json.Marshal(ret)
	_, _, _, _ = s.onWriterResult(w, r, buffer)
}

func (s *ServerJson) onReadFilter(w ht.ResponseWriter, r *ht.Request, buffer []byte) (ht.ResponseWriter, *ht.Request, []byte, error) {
	buffer, err := ioutil.ReadAll(r.Body)
	if nil != err {
		return nil, nil, nil, &Error{Code: ht.StatusInternalServerError}
	}

	for _, filter := range s.readFilter {
		w, r, buffer, err = filter(w, r, buffer)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	return w, r, buffer, nil
}

func (s *ServerJson) onErrorFilter(w ht.ResponseWriter, r *ht.Request, e error) *hbuf.Result {
	if nil != s.errorFilter {
		e = s.errorFilter(w, r, e)
	}
	if nil == e {
		return nil
	}
	println("Error: %s", e.Error())
	switch e.(type) {
	case *hbuf.Result:
		buffer, err := json.Marshal(e.(*hbuf.Result))
		if err != nil {
			w.WriteHeader(ht.StatusInternalServerError)
			return nil
		}
		_, _, _, _ = s.onWriterResult(w, r, buffer)
	case *Error:
		switch e.(*Error).Code {
		case ht.StatusNotFound, ht.StatusInternalServerError:
			w.WriteHeader(e.(*Error).Code)
			return nil
		}
	}
	w.WriteHeader(ht.StatusInternalServerError)
	return nil
}

func (s *ServerJson) onWriterResult(w ht.ResponseWriter, r *ht.Request, buffer []byte) (ht.ResponseWriter, *ht.Request, []byte, error) {
	var err error
	for _, filter := range s.writerFilter {
		w, r, buffer, err = filter(w, r, buffer)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	_, err = w.Write(buffer)
	if err != nil {
		w.WriteHeader(ht.StatusInternalServerError)
		println("ResponseWriter Error: %s", err.Error())
		return nil, nil, nil, err
	}
	return nil, nil, nil, nil
}
