package http

import (
	"context"
	"encoding/json"
	"hbuf_golang/pkg/hbuf"
)

type People struct {
}

type GetNameReq struct {
}

func (g *GetNameReq) ToData() ([]byte, error) {
	return json.Marshal(g)
}

func (g *GetNameReq) FormData(data []byte) error {
	return json.Unmarshal(data, g)
}

type GetNameRes struct {
	Name string `json:"name"`
}

func (g *GetNameRes) ToData() ([]byte, error) {
	return json.Marshal(g)
}

func (g *GetNameRes) FormData(data []byte) error {
	return json.Unmarshal(data, g)
}

type PeopleServer interface {
	GetName(cxt context.Context, req *GetNameReq) (*GetNameRes, error)
}

type PeopleRouter struct {
	people PeopleServer
	names  map[string]*hbuf.ServerInvoke
}

func NewPeopleRouter(people PeopleServer) *PeopleRouter {
	return &PeopleRouter{
		people: people,
		names: map[string]*hbuf.ServerInvoke{
			"people/get_name": {
				ToData: func(buf []byte) (hbuf.Data, error) {
					var req GetNameReq
					return &req, json.Unmarshal(buf, &req)
				},
				FormData: func(data hbuf.Data) ([]byte, error) {
					return json.Marshal(&data)
				},
				Invoke: func(cxt context.Context, data hbuf.Data) (hbuf.Data, error) {
					hbuf.SetTag(cxt, "auth", []string{"1", ""})
					return people.GetName(cxt, data.(*GetNameReq))
				},
			},
		},
	}
}

func (p *PeopleRouter) GetName() string {
	return "people"
}

func (p *PeopleRouter) GetId() uint32 {
	return 1
}

func (p *PeopleRouter) GetInvoke() map[string]*hbuf.ServerInvoke {
	return p.names
}
