package hbuf

import "io"

type Data interface {
	Encoder(w io.Writer) (err error)
	Decoder(r io.Reader) (err error)
}

type Int64 int64
