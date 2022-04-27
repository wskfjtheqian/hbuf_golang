package hbuf_golang

type Data interface {
	ToData() ([]byte, error)
}
