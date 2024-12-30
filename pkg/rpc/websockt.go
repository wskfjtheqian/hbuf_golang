package rpc

import "golang.org/x/net/websocket"

type WebSocket struct {
	conn *websocket.Conn
}

func (ws *WebSocket) run() {

}
