package rpc

import (
	"github.com/gobwas/ws"
	"net"
)

type WebSocket struct {
	conn net.Conn
}

func (ws *WebSocket) run() {

}

////////////////////////////////////////////////////////////////////////////////////////////////////////////////////////

func NewWebSocketClient(baseURL string) *WebSocketClient {

	return &WebSocketClient{}
}

type WebSocketClient struct {
	conn   *ws.Dialer
	filter ResponseFilter
}

// //////////////////////////////////////////////////////////////////////////////////////////////////////////////////////
func NewWebSocketServer(addr string) *WebSocketServer {
	return &WebSocketServer{}
}

type WebSocketServer struct {
	listener net.Listener
}
