package http

import (
	"hbuf_golang/pkg/hbuf"
	h "net/http"
	"testing"
)

type PeopleImp struct {
}

func (p PeopleImp) GetName(cxt *hbuf.Context, req *GetNameReq) (*GetNameRes, error) {
	return &GetNameRes{
		Name: "何乾",
	}, nil
}

func Test_Server(t *testing.T) {

	router := NewServerJson()
	router.add(NewPeopleRouter(&PeopleImp{}))

	h.HandleFunc("/", router.ServeHTTP)
	h.ListenAndServe(":8080", nil)
}
