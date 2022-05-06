package http

import (
	ht "net/http"
)

type HttpError struct {
	code int
	msg  string
}

func (h *HttpError) Error() string {
	return h.msg
}

func NewHttpError(code int, msg string) *HttpError {
	return &HttpError{
		code: code,
		msg:  msg,
	}
}

func NewHttpErrorByCode(code int) *HttpError {
	return &HttpError{
		code: code,
		msg:  ht.StatusText(code),
	}
}
