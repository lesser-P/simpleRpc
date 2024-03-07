package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"simpleRpc"
	"simpleRpc/client"
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
	l, err := net.Listen("tcp", ":9999")
	if err != nil {
		log.Fatal("network error:", err)
	}
	if err = simpleRpc.Register(&foo); err != nil {
		log.Fatal("register error:", err)
	}
	// 在随机的端口创建一个监听
	simpleRpc.HandleHTTP()
	log.Println("start rpc server on", l.Addr())
	addr <- l.Addr().String()
	//simpleRpc.Accept(l)
	_ = http.Serve(l, nil)
}

func call(addrCh chan string) {
	cli, _ := client.DialHTTP("tcp", <-addrCh, &simpleRpc.Option{ConnectTimeout: 10 * time.Second})
	defer func() { _ = cli.Close() }()

	time.Sleep(time.Second)

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			args := &Args{Num1: i, Num2: i * i}
			var reply int
			if err := cli.Call(context.Background(), "Foo.Sum", args, &reply); err != nil {
				log.Fatal("call Foo.Sum error: ", err)
			}
			log.Printf("%d + %d = %d", args.Num1, args.Num2, reply)
		}(i)
	}
	wg.Wait()
}

func main() {
	log.SetFlags(0)
	addr := make(chan string)
	go startServer(addr)

	call(addr)
	// 启动服务端

	// 启动客户端
	//cli, _ := client.Dial("tcp", <-addr)
	//
	//defer func() { _ = cli.Close() }()
	//time.Sleep(time.Second)
	//// send request receive response
	//var wg sync.WaitGroup
	//// 发起5次调用
	//for i := 0; i < 5; i++ {
	//	wg.Add(1)
	//	go func(i int) {
	//		defer wg.Done()
	//		args := &Args{Num1: i, Num2: i * i}
	//		var reply int
	//		// 发起调用
	//		if err := cli.Call(context.Background(), "Foo.Sum", args, &reply); err != nil {
	//			log.Fatal("call Foo.Sum error:", err)
	//		}
	//		log.Printf("%d + %d = %d:", args.Num1, args.Num2, reply)
	//	}(i)
	//}
	//wg.Wait()
}
