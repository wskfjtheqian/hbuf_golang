package mian

import "net/http"

func mian() {
	handler := http.HttpServerRouter{}
	http.ListenAndServe(":8040", handler)
}
