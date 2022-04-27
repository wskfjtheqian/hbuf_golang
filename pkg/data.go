package hbuf_golang

type Data interface {
	toMap() map[string]interface{}

	toData() []byte
}
