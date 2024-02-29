package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"simpleRpc"
	"simpleRpc/codec"
	"time"
)

func startServer(addr chan string) {
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatal("network error:", err)
	}
	log.Println("start rpc server on", l.Addr())
	addr <- l.Addr().String()
	simpleRpc.Accept(l)
}

func main() {
	addr := make(chan string)
	go startServer(addr)

	conn, _ := net.Dial("tcp", <-addr)
	defer func() { conn.Close() }()
	time.Sleep(time.Second)
	// send options
	// 把DefaultOption编码成Json格式添加到conn中并通过conn网络连接发送出去
	_ = json.NewEncoder(conn).Encode(simpleRpc.DefaultOption)
	cc := codec.NewGobCodec(conn)
	for i := 0; i < 5; i++ {
		h := &codec.Header{
			ServiceMethod: "Foo.Sum",
			Seq:           uint64(i),
		}
		_ = cc.Write(h, fmt.Sprintf("simpleRpc req %d", h.Seq))
		_ = cc.ReadHeader(h)
		var reply string
		_ = cc.ReadBody(&reply)
		log.Println("reply:", reply)
	}

}
