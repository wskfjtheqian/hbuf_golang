package main

import (
	hbuf_http "hbuf_golang/http"
	hbuf "hbuf_golang/pkg"
	"net/http"
)

type Pep struct {
	Dd string
}

func (p Pep) ToData() ([]byte, error) {
	return nil, nil
}

type Ser struct {
}

func (s Ser) InvokeMap() map[string]hbuf.InvokeMap {
	return map[string]hbuf.InvokeMap{}
}

func (s Ser) InvokeData() map[int64]hbuf.InvokeData {
	return map[int64]hbuf.InvokeData{}
}

func (s Ser) Name() string {
	return "Ser"
}

func (s Ser) Id() uint16 {
	return 0
}

func main() {
	var data hbuf.Data = Pep{}
	data.ToData()

	handler := hbuf_http.NewHttpServer()
	handler.AddServer(&Ser{})

	err := http.ListenAndServe(":8045", handler)
	if err != nil {
		return
	}

}
