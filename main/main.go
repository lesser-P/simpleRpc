package main

import (
	"fmt"
	"log"
	"net"
	"simpleRpc/client"
	"simpleRpc/service"
	"sync"
	"time"
)

type Foo int
type Args struct {
	Num1, Num2 int
}

func (f Foo) Sum(args Args, reply *int) error {
	*reply = args.Num1 + args.Num2
	return nil
}

func startServer(addr chan string) {
	var foo Foo
	// 注册到服务中

	// 在随机的端口创建一个监听
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		log.Fatal("network error:", err)
	}
	log.Println("start rpc server on", l.Addr())
	addr <- l.Addr().String()
	service.Accept(l)
}

func main() {
	log.SetFlags(0)
	addr := make(chan string)
	// 启动服务端
	go startServer(addr)

	// 启动客户端
	cli, _ := client.Dial("tcp", <-addr)

	defer func() { _ = cli.Close() }()
	time.Sleep(time.Second)
	// send request receive response
	var wg sync.WaitGroup
	// 发起5次调用
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
