package hbuf

type Server interface {
	getName() string

	getId() uint32
}

type ServerRoute interface {
}
