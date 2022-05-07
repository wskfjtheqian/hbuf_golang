package hbuf

import "context"

type Context struct {
	context.Context
}

type ServerInvoke struct {
	ToData func(buf []byte) (Data, error)

	FormData func(data Data) ([]byte, error)

	Invoke func(cxt *Context, data Data) (Data, error)
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
