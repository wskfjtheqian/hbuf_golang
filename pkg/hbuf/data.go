package hbuf

type Data interface {
	ToData() ([]byte, error)

	FormData([]byte) error
}
