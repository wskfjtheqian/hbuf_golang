package pkg

import (
	"net"
	"testing"
)

// TestFunc 监听空闲端口和获得IP地址和端口的测试函数
func TestFunc(t *testing.T) {
	listen, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Error(err)
		return
	}

	port, s, err := net.SplitHostPort(listen.Addr().String())
	if err != nil {
		t.Error(err)
		return

	}
	t.Log("Listen on ", port)
	t.Log("IP address is ", s)

}

// 获得空闲端口后
func TestFunc2(t *testing.T) {
	address, err := net.ResolveTCPAddr("tcp", ":0")
	if err != nil {
		t.Error(err)
		return
	}
	ip := address.IP.String()
	port := address.Port
	t.Log("Listen on ", port)
	t.Log("IP address is ", ip)
}
