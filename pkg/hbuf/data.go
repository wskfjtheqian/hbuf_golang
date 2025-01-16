package hbuf

import "io"

// Data 接口，它包装了 Encoder 和 Decoder 方法
type Data interface {
	// Encoder 将数据编码到 io.Writer 中
	Encoder(w io.Writer) error
	// Decoder 从 io.Reader 中解码数据
	Decoder(r io.Reader) error
	// Size 返回数据大小
	Size() int
}

type Int64 int64
