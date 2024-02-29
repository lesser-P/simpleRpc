package main

import (
	"fmt"
	"log"
	"net"
	"simpleRpc"
	"simpleRpc/client"
	"sync"
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
	log.SetFlags(0)
	addr := make(chan string)
	go startServer(addr)
	cli, _ := client.Dial("tcp", <-addr)

	defer func() { _ = cli.Close() }()
	time.Sleep(time.Second)
	// send request receive response
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			args := fmt.Sprintf("simpleRpc req %d", i)
			var reply string
			// 发起调用
			if err := cli.Call("Foo.Sum", args, &reply); err != nil {
				log.Fatal("call Foo.Sum error:", err)
			}
			log.Println("reply:", reply)
		}(i)
	}
	wg.Wait()
}
