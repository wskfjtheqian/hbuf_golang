package rpc

import (
	"context"
	"encoding/json"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hbuf"
	"net/http"
	"reflect"
	"testing"
)

type NameReq struct {
	Id string `json:"id"`
}

func (d *NameReq) ToData() ([]byte, error) {
	return nil, nil
}

func (d *NameReq) FormData([]byte) error {
	return nil
}

type NameReps struct {
	Name string `json:"name"`
}

func (d *NameReps) ToData() ([]byte, error) {
	return nil, nil
}

func (d *NameReps) FormData([]byte) error {
	return nil
}

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

type RpcTest interface {
	Init()

	GetName(ctx context.Context, req *NameReq) (*NameReps, error)
}

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

type RpcTestClient struct {
	client Client
}

func NewRpcTestClient(client Client) *RpcTestClient {
	return &RpcTestClient{
		client: client,
	}
}

func (r *RpcTestClient) Init() {

}

func (r *RpcTestClient) GetName(ctx context.Context, req *NameReq) (*NameReps, error) {
	ret, err := r.client.Invoke(ctx, req, "rpc_test/rpc_test/get_name", &ClientInvoke{
		ToData: func(buf []byte) (hbuf.Data, error) {
			var req NameReps
			return &req, json.Unmarshal(buf, &req)
		},
		FormData: func(data hbuf.Data) ([]byte, error) {
			return json.Marshal(&data)
		},
	}, 1, &ClientInvoke{})
	if err != nil {
		return nil, err
	}
	return ret.(*NameReps), nil
}

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

type RpcTestRouter struct {
	server RpcTest
	names  map[string]*ServerInvoke
}

func (r *RpcTestRouter) GetName() string {
	return "rpc_test"
}

func (r *RpcTestRouter) GetId() uint32 {
	return 1
}

func (r *RpcTestRouter) GetInvoke() map[string]*ServerInvoke {
	return r.names
}

func (r *RpcTestRouter) GetServer() Init {
	return r.server
}

func NewRpcTestRouter(server RpcTest) *RpcTestRouter {
	return &RpcTestRouter{
		server: server,
		names: map[string]*ServerInvoke{
			"rpc_test/get_name": {
				ToData: func(buf []byte) (hbuf.Data, error) {
					var req NameReq
					return &req, json.Unmarshal(buf, &req)
				},
				FormData: func(data hbuf.Data) ([]byte, error) {
					return json.Marshal(&data)
				},
				SetInfo: func(ctx context.Context) {
				},
				Invoke: func(ctx context.Context, data hbuf.Data) (hbuf.Data, error) {
					return server.GetName(ctx, data.(*NameReq))
				},
			},
		},
	}
}

//////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

type RpcTestServer struct {
}

func (r RpcTestServer) Init() {

}

func (r RpcTestServer) GetName(ctx context.Context, req *NameReq) (*NameReps, error) {
	return &NameReps{
		Name: "1000",
	}, nil
}

func Test_HttpClient(t *testing.T) {
	client := NewClientHttp("http://127.0.0.1:8901/api/")
	jsonClient := NewJsonClient(client)
	api := NewRpcTestClient(jsonClient)
	name, err := api.GetName(context.TODO(), &NameReq{Id: "111"})
	if err != nil {
		print(err)
		return
	}
	print(name.Name)
}

func Test_HttpServer(t *testing.T) {
	rpc := NewServer()
	rpc.Add(NewRpcTestRouter(RpcTestServer{}))

	jsonRpc := NewServerJson(rpc)
	httpApi := NewServerHttp("/api", jsonRpc)
	http.Handle("/api/", httpApi)
	http.ListenAndServe(":8901", nil)
}

func Test_WebSocketClient(t *testing.T) {
	client := NewClientWebSocket("ws://127.0.0.1:8901/api/")
	jsonClient := NewJsonClient(client)
	api := NewRpcTestClient(jsonClient)

	for i := 0; i < 5; i++ {
		name, err := api.GetName(context.TODO(), &NameReq{Id: "111"})
		if err != nil {
			print(err)
			return
		}
		println(name.Name)
	}
}

func Test_WebSocketServer(t *testing.T) {
	rpc := NewServer()
	rpc.Add(NewRpcTestRouter(RpcTestServer{}))

	jsonRpc := NewServerJson(rpc)
	httpApi := NewServerWebSocket(jsonRpc)
	http.Handle("/api/", httpApi)
	http.ListenAndServe(":8901", nil)
}

type AAA func(t *testing.T)

func Test_P(t *testing.T) {
	var a AAA = Test_WebSocketServer
	if reflect.DeepEqual(a, Test_WebSocketServer) {
		println("")
	}
}
