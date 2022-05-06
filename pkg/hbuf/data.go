package hbuf

type Data interface {
	toData() ([]byte, error)
}
