package client

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"simpleRpc"
	"simpleRpc/codec"
	"strings"
	"sync"
	"time"
)

// 承载一次RPC调用所需要的信息
type Call struct {
	Seq           uint64
	ServiceMethod string
	Args          interface{}
	Reply         interface{}
	Error         error
	Done          chan *Call // call执行完毕后，保存到Done中
}

func (call *Call) done() {
	// 为的是告诉其他协程这个call完成了
	call.Done <- call
}

type Client struct {
	cc       codec.Codec // 编解码器
	opt      *simpleRpc.Option
	sending  sync.Mutex
	header   codec.Header
	mu       sync.Mutex
	seq      uint64
	pending  map[uint64]*Call //存储未处理完的请求，键是编号，值是Call实例
	closing  bool             //user has called Close
	shutdown bool             //server has told us to stop 一般是有错误发生
}
type newClientFunc func(conn net.Conn, opt *simpleRpc.Option) (client *Client, err error)

func dialTimeout(f newClientFunc, network, address string, opts ...*simpleRpc.Option) (client *Client, err error) {
	opt, err := parseOptions(opts...)
	if err != nil {
		return nil, err
	}
	conn, err := net.Dial(network, address)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = conn.Close()
	}()
	ch := make(chan clientResult)
	// 异步创建client
	go func() {
		client, err = f(conn, opt)
		// 将client和err发送到ch中
		ch <- clientResult{client, err}
	}()
	// 如果没有设置超时时间，则直接等待
	if opt.ConnectTimeout == 0 {
		result := <-ch
		return result.client, result.err
	}

	// 阻塞直到有一个通道传入数据
	select {
	// 超时
	case <-time.After(opt.ConnectTimeout):
		return nil, fmt.Errorf("rpc client: connect timeout:expect within %s", opt.ConnectTimeout)
		// 没超时
	case result := <-ch:
		return result.client, result.err
	}
}

func Dial(network, address string, opts ...*simpleRpc.Option) (*Client, error) {
	return dialTimeout(NewClient, network, address, opts...)
}

var _ io.Closer = (*Client)(nil)

var ErrShutdown = errors.New("connection is shut down")

// Close the connection
func (client *Client) Close() error {
	client.mu.Lock()
	defer client.mu.Unlock()
	// 如果closing为true则表示客户端已经在关闭过程中了
	if client.closing {
		return ErrShutdown
	}
	client.closing = true
	return client.cc.Close()
}

// 注册请求，把call实例保存到pending中
func (client *Client) registerCall(call *Call) (uint64, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.closing || client.shutdown {
		return 0, ErrShutdown
	}
	call.Seq = client.seq
	client.pending[call.Seq] = call
	client.seq++
	return call.Seq, nil
}

// removeCall 从pending中删除call实例，返回删除的call
func (client *Client) removeCall(seq uint64) *Call {
	client.mu.Lock()
	defer client.mu.Unlock()
	call := client.pending[seq]
	delete(client.pending, seq)
	return call
}

// 服务端或客户端发生错误时调用，将shutdown设置为true，且将错误信息通知所有pending状态的call，要同时保证状态和发送过程同步，不能并发进行
func (client *Client) terminateCalls(err error) {
	client.sending.Lock()
	defer client.sending.Unlock()
	client.mu.Lock()
	defer client.mu.Unlock()
	client.shutdown = true
	for _, call := range client.pending {
		call.Error = err
		call.done()
	}
}

func (client *Client) receive() {
	var err error
	for err == nil {
		var h codec.Header
		if err = client.cc.ReadHeader(&h); err != nil {
			break
		}
		// 把call取出
		call := client.removeCall(h.Seq)
		//call 不存在，可能是请求没有发送完整，或者因为其他原因被取消，但是服务端仍旧处理了。
		//call 存在，但服务端处理出错，即 h.Error 不为空。
		//call 存在，服务端处理正常，那么需要从 body 中读取 Reply 的值。
		switch {
		case call == nil:
			err = client.cc.ReadBody(nil)
		case h.Error != "":
			call.Error = fmt.Errorf(h.Error)
			err = client.cc.ReadBody(nil)
			call.done()
		default:
			err = client.cc.ReadBody(call.Reply)
			if err != nil {
				call.Error = errors.New("reading body " + err.Error())
			}
			// 结束call
			call.done()
		}
	}
	client.terminateCalls(err)
}

func NewClient(conn net.Conn, opt *simpleRpc.Option) (*Client, error) {
	f := codec.NewCodecFuncMap[opt.CodeType]
	if f == nil {
		err := fmt.Errorf("invalid code type %s", opt.CodeType)
		log.Println("rpc client: client error:", err)
		return nil, err
	}
	if err := json.NewEncoder(conn).Encode(opt); err != nil {
		log.Println("rpc client: options error:", err)
		_ = conn.Close()
		return nil, err
	}
	return newClientCodec(f(conn), opt), nil
}

func newClientCodec(cc codec.Codec, opt *simpleRpc.Option) *Client {
	client := &Client{
		seq:     1,
		cc:      cc,
		opt:     opt,
		pending: make(map[uint64]*Call),
	}
	go client.receive()
	return client
}

func parseOptions(opts ...*simpleRpc.Option) (*simpleRpc.Option, error) {
	if len(opts) == 0 || opts[0] == nil {
		return simpleRpc.DefaultOption, nil
	}
	if len(opts) != 1 {
		return nil, errors.New("number of options is more than 1")
	}
	opt := opts[0]
	opt.MagicNumber = simpleRpc.DefaultOption.MagicNumber
	if opt.CodeType == "" {
		opt.CodeType = simpleRpc.DefaultOption.CodeType
	}
	return opt, nil
}

func (client *Client) send(call *Call) {
	client.sending.Lock()
	defer client.sending.Unlock()

	// 注册请求
	seq, err := client.registerCall(call)
	if err != nil {
		call.Error = err
		call.done()
		return
	}
	// 准备请求头
	client.header.ServiceMethod = call.ServiceMethod
	client.header.Seq = seq
	client.header.Error = ""

	// 编码并发送
	if err := client.cc.Write(&client.header, call.Args); err != nil {
		call := client.removeCall(seq)
		if call != nil {
			call.Error = err
			call.done()
		}
	}
}

// Go 是异步调用，返回call实例
func (client *Client) Go(serviceMethod string, args, reply interface{}, done chan *Call) *Call {
	if done == nil {
		done = make(chan *Call, 10)
	} else if cap(done) == 0 {
		log.Panic("rpc client: done channel is unbuffered")
	}
	call := &Call{
		ServiceMethod: serviceMethod,
		Args:          args,
		Reply:         reply,
		Done:          done,
	}
	client.send(call)
	return call
}

// Call 是同步调用
func (client *Client) Call(ctx context.Context, serviceMethod string, args, reply interface{}) error {
	// 返回执行完毕的call
	call := client.Go(serviceMethod, args, reply, make(chan *Call, 1))
	select {
	// 根据上下文来决定调用Done的时机
	// 用户可以使用 context.WithTimeout 创建具备超时检测能力的 context 对象来控制
	case <-ctx.Done():
		client.removeCall(call.Seq)
		return errors.New("rpc client: call faild: " + ctx.Err().Error())
	case call := <-call.Done:
		return call.Error
	}
}

type clientResult struct {
	client *Client
	err    error
}

const (
	connected        = "200 Connected to simple rpc"
	defaultRPCPath   = "/_simperpc_"
	defaultDebugpath = "/debug/simplerpc"
)

// 客户端支持HTTP协议
func NewHTTPClient(conn net.Conn, opt *simpleRpc.Option) (*Client, error) {
	_, _ = io.WriteString(conn, fmt.Sprintf("CONNECT %s HTTP/1.0\n\n", defaultRPCPath))

	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "CONNECT"})
	if err == nil && resp.Status == connected {
		return NewClient(conn, opt)
	}
	if err == nil {
		err = errors.New("unexpected HTTP response: " + resp.Status)
	}
	return nil, err
}

// DialHttp
func DialHTTP(network, address string, opts ...*simpleRpc.Option) (*Client, error) {
	return dialTimeout(NewHTTPClient, network, address, opts...)
}

// XDial
func XDial(rpcAddr string, opts ...*simpleRpc.Option) (*Client, error) {
	parts := strings.Split(rpcAddr, "@")
	if len(parts) != 2 {
		return nil, fmt.Errorf("rpc client:invalid rpc address %s", rpcAddr)
	}
	protocol, addr := parts[0], parts[1]
	switch protocol {
	case "http":
		return DialHTTP("tcp", addr, opts...)
	default:
		return Dial(protocol, addr, opts...)
	}
}
