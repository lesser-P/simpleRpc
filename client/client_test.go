package client

import (
	"context"
	"fmt"
	"net"
	"os"
	"runtime"
	"simpleRpc"
	"strings"
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

	f := func(conn net.Conn, opt *simpleRpc.Option) (client *Client, err error) {
		_ = conn.Close()
		time.Sleep(5 * time.Second)
		return nil, nil
	}

	t.Run("timeout", func(t *testing.T) {
		_, err := dialTimeout(f, "tcp", l.Addr().String(), &simpleRpc.Option{ConnectTimeout: time.Second})
		_assert(err != nil && strings.Contains(err.Error(), "connect timeout"), "expect a timeout error")
	})
	t.Run("0", func(t *testing.T) {
		_, err := dialTimeout(f, "tcp", l.Addr().String(), &simpleRpc.Option{ConnectTimeout: 0})
		_assert(err == nil, "0 means no limit")
	})
}

type Bar int

func (b Bar) Timeout(argv int, reply *int) error {
	time.Sleep(2 * time.Second)
	return nil
}
func startServer(addr chan string) {
	var b Bar
	_ = simpleRpc.Register(b)
	l, _ := net.Listen("tcp", ":0")
	addr <- l.Addr().String()
}

func TestClient_Call(t *testing.T) {
	t.Parallel()
	addrCh := make(chan string)
	go startServer(addrCh)

	addr := <-addrCh
	time.Sleep(time.Second)
	t.Run("client timeout", func(t *testing.T) {
		client, _ := Dial("tcp", addr)
		ctx, _ := context.WithTimeout(context.Background(), time.Second)
		var reply int
		err := client.Call(ctx, "Bar.Timeout", 1, &reply)
		_assert(err != nil && strings.Contains(err.Error(), "client timeout"), err.Error())
	})
}

func TestXDial(t *testing.T) {
	if runtime.GOOS == "linux" {
		ch := make(chan struct{})
		addr := "/tmp/simplerpc.sock"
		go func() {
			_ = os.Remove(addr)
			l, err := net.Listen("unix", addr)
			if err != nil {
				t.Fatal("faild to listen unix socket")
			}
			ch <- struct{}{}
			simpleRpc.Accept(l)
		}()
		<-ch
		_, err := XDial("unix@" + addr)
		_assert(err == nil, "failed to connect unix socket")
	}
}
