package client

import (
	"fmt"
	"net"
	"simpleRpc/service"
	"testing"
	"time"
)

func _assert(condition bool, msg string, v ...interface{}) {
	if !condition {
		panic(fmt.Sprintf("assertion failed: "+msg, v...))
	}
}
func TestClient_dialTimeout(t *testing.T) {
	t.Parallel()

	l, _ := net.Listen("tcp", ":0")
	f := func(conn net.Conn, opt *service.Option) (client *Client, err error) {
		conn.Close()
		time.Sleep(2 * time.Second)
		return nil, nil
	}

	t.Run("func timeout", func(t *testing.T) {
		_, err := dialTimeout(f, l.Addr().Network(), l.Addr().String(), &service.Option{ConnectTimeout: time.Second})
		_assert(err != nil, "expect a timeout error")
	})

}
