package rpc_test

import (
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"reflect"
	"unsafe"
)

var hbufRequest HbufRequest
var hbufRequestDescriptor = hbuf.NewDataDescriptor(0, false, reflect.TypeOf(&hbufRequest), map[uint16]hbuf.Descriptor{}, map[uint16]hbuf.Descriptor{
	1: hbuf.NewStringDescriptor(unsafe.Offsetof(hbufRequest.Name), false),
	2: hbuf.NewInt32Descriptor(unsafe.Offsetof(hbufRequest.Age), false),
})

type HbufRequest struct {
	Name string `json:"name,omitempty" hbuf:"1"` //
	Age  int32  `json:"age,omitempty" hbuf:"2"`  //
}

func (g *HbufRequest) Descriptors() hbuf.Descriptor {
	return hbufRequestDescriptor
}

func (g *HbufRequest) GetName() string {
	return g.Name
}

func (g *HbufRequest) SetName(val string) {
	g.Name = val
}

func (g *HbufRequest) GetAge() int32 {
	return g.Age
}

func (g *HbufRequest) SetAge(val int32) {
	g.Age = val
}

var hbufResponse HbufResponse
var hbufResponseDescriptor = hbuf.NewDataDescriptor(0, false, reflect.TypeOf(&hbufResponse), map[uint16]hbuf.Descriptor{}, map[uint16]hbuf.Descriptor{
	1: hbuf.NewStringDescriptor(unsafe.Offsetof(hbufResponse.Name), false),
	2: hbuf.NewInt32Descriptor(unsafe.Offsetof(hbufResponse.Age), false),
})

type HbufResponse struct {
	Name string `json:"name,omitempty" hbuf:"1"` //
	Age  int32  `json:"age,omitempty" hbuf:"2"`  //
}

func (g *HbufResponse) Descriptors() hbuf.Descriptor {
	return hbufResponseDescriptor
}

func (g *HbufResponse) GetName() string {
	return g.Name
}

func (g *HbufResponse) SetName(val string) {
	g.Name = val
}

func (g *HbufResponse) GetAge() int32 {
	return g.Age
}

func (g *HbufResponse) SetAge(val int32) {
	g.Age = val
}
