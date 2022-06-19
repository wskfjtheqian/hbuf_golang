package hbuf

import "context"

type Result struct {
	Code int         `json:"code"`
	Msg  string      `json:"msg"`
	Data interface{} `json:"data"`
}

func (h *Result) Error() string {
	return h.Msg
}

type ServerInvoke struct {
	ToData func(buf []byte) (Data, error)

	FormData func(data Data) ([]byte, error)

	Invoke func(cxt context.Context, data Data) (Data, error)
}

type ServerClient interface {
	GetName() string

	GetId() uint32
}

type ServerRouter interface {
	GetName() string

	GetId() uint32

	GetInvoke() map[string]*ServerInvoke
}

type GetServer interface {
	Get(router ServerClient) interface{}
}
