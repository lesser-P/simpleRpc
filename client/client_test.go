package client

import (
	"context"
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
	//
	t.Run("func timeout", func(t *testing.T) {
		_, err := dialTimeout(f, l.Addr().Network(), l.Addr().String(), &service.Option{ConnectTimeout: time.Second})
		_assert(err != nil, "expect a timeout error")
	})
}

type Bar int

func (b *Bar) Timeout(argv int, reply *int) error {
	time.Sleep(2 * time.Second)
	return nil
}
func startServer(addr chan string) {
	var b Bar
	_ = service.Register(&b)
	l, _ := net.Listen("tcp", ":0")
	addr <- l.Addr().String()
	service.Accept(l)
}

func TestClient_Call(t *testing.T) {
	t.Parallel()
	addrCh := make(chan string)
	go startServer(addrCh)
	addr := <-addrCh
	time.Sleep(time.Second)
	t.Run("client timeout", func(t *testing.T) {
		client, _ := Dial("tcp", addr, nil)
		ctx, _ := context.WithTimeout(context.Background(), time.Second)
		var reply int
		client.Call(ctx, "Bar.Timeout", 1, &reply)
	})
}
