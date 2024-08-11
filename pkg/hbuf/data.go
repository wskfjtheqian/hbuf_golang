package hbuf

import "io"

type EncoderCall func(io.Writer) error

type Data interface {
	Encoder(id uint16) (uint32, EncoderCall)
}

type String string

func (h *String) Encoder(id uint16) (uint32, EncoderCall) {
	if nil == h || len(*h) == 0 {
		return 0, nil
	}
	strLen := len(*h)
	length := LengthInt64(int64(strLen))

	return uint32(strLen) + uint32(length) + 2, func(w io.Writer) error {
		err := WriterField(w, TBytes, id, length)
		if err != nil {
			return err
		}

		b := EncoderInt64(int64(length))
		_, err = w.Write(b)
		if err != nil {
			return err
		}
		_, err = w.Write([]byte(*h))
		return err
	}
}

type Int8 int8

func (h *Int8) Encoder(id uint16) (uint32, EncoderCall) {
	if nil == h || *h == 0 {
		return 0, nil
	}

	length := LengthInt64(int64(*h))

	return uint32(length) + 1, func(w io.Writer) error {
		err := WriterField(w, TBytes, id, length)
		if err != nil {
			return err
		}

		b := EncoderInt64(int64(*h))
		_, err = w.Write(b)
		return err
	}
}

func EncoderList[E Data](id uint16, v []E) (uint32, EncoderCall) {
	if nil == v || len(v) == 0 {
		return 0, nil
	}

	var tempLen, length, count uint32
	var call EncoderCall
	var calls = make([]EncoderCall, len(v))

	for _, item := range v {
		tempLen, call = item.Encoder(0)
		if nil != calls {
			length += tempLen
			calls[count] = call
		}
	}

	count = uint32(LengthInt64(int64(len(v))))
	tempLen := LengthInt64(int64(length))

	return uint32(length), func(w io.Writer) error {
		err := WriterField(w, TList, id, length)
		if err != nil {
			return err
		}

		b := EncoderInt64(int64(*h))
		_, err = w.Write(b)

		for _, call = range calls {
			err = call(w)
			if err != nil {
				return err
			}
		}
		return err
	}
}
