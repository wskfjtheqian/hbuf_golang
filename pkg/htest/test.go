package htest

import (
	"context"
	"encoding/json"
	"github.com/wskfjtheqian/hbuf_golang/pkg/erro"
	"github.com/wskfjtheqian/hbuf_golang/pkg/rpc"
	"testing"
)

var Context func() context.Context = func() context.Context {
	return rpc.NewContext(context.TODO())
}

func HTest(desc string, t *testing.T, call func(ctx context.Context) (any, error)) {
	val, err := call(Context())
	if err != nil {
		t.Error(err.Error())
		return
	}
	marshal, err := json.MarshalIndent(&val, "", "\t")
	if err != nil {
		erro.PrintStack(err)
		return
	}
	t.Log(string(marshal))
}
