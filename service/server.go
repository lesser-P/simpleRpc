package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"reflect"
	"simpleRpc/codec"
	"strings"
	"sync"
	"time"
)

const MagicNumber = 0x3bef5c

type Option struct {
	MagicNumber    int
	CodeType       codec.Type
	ConnectTimeout time.Duration
	HandleTimeout  time.Duration
}
type request struct {
	h            *codec.Header
	argv, replyv reflect.Value // 请求的参数和返回值
	mtype        *methodType
	svc          *service
}

// 编解码方式
var DefaultOption = &Option{
	MagicNumber:    MagicNumber,
	CodeType:       codec.GobType,
	ConnectTimeout: time.Second * 10, // 连接超时10s
}

// Option由JSON来编解码，Header和Body由CodeType来决定编解码方式
type Server struct {
	// 同步映射，用于存储服务名称到服务实例的映射
	serviceMap sync.Map
}

func NewServer() *Server {
	return &Server{}
}

var DefaultServer = NewServer()

func (s *Server) Accept(lis net.Listener) {
	for {
		// 接收传入的网络连接
		conn, err := lis.Accept()
		if err != nil {
			log.Println("rpc server: accept error:", err)
			return
		}
		go s.ServerConn(conn)
	}
}

func Accept(lis net.Listener) {
	DefaultServer.Accept(lis)
}

// io.ReadWriteCloser是一个接口，包含了io.Reader和io.Writer接口
// 这意味着你可以传入一个网络连接（例如 net.Conn），也可以传入一个文件（例如 os.File），
// 只要这个对象实现了 io.ReadWriteCloser 接口就可以。
func (server *Server) ServerConn(conn io.ReadWriteCloser) {
	defer func() {
		_ = conn.Close()
	}()
	var opt Option
	// 反序列化得到opt实例
	if err := json.NewDecoder(conn).Decode(&opt); err != nil {
		log.Println("rpc server: options error:", err)
		return
	}
	if opt.MagicNumber != MagicNumber {
		log.Printf("rpc server: invalid magic number %x", opt.MagicNumber)
		return
	}

	if opt.CodeType != codec.GobType && opt.CodeType != codec.JsonType {
		log.Printf("rpc server: invalid code type %s", opt.CodeType)
		return
	}
	f := codec.NewCodecFuncMap[opt.CodeType]
	if f == nil {
		log.Printf("rpc server: invalid code type %s", opt.CodeType)
		return
	}
	server.serverCodec(f(conn))
}

var invalidRequest = struct{}{}

func (server *Server) serverCodec(cc codec.Codec) {
	send := new(sync.Mutex)
	wg := new(sync.WaitGroup)
	for {
		req, err := server.readRequest(cc)
		if err != nil {
			if req == nil {
				break
			}
			req.h.Error = err.Error()
			server.sendResponse(cc, req.h, invalidRequest, send)
			continue
		}
		wg.Add(1)
		go server.handleRequest(cc, req, send, wg)
	}
	wg.Wait()
	_ = cc.Close()
}

// 读取请求头
func (server *Server) readRequestHeader(cc codec.Codec) (*codec.Header, error) {
	var h codec.Header
	// gob编解码
	if err := cc.ReadHeader(&h); err != nil {
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			log.Println("rpc server: read header error:", err)
		}
		return nil, err
	}
	return &h, nil
}

// 读取请求
func (server *Server) readRequest(cc codec.Codec) (*request, error) {
	// 解析请求头
	h, err := server.readRequestHeader(cc)
	if err != nil {
		return nil, err
	}
	req := &request{h: h}
	req.svc, req.mtype, err = server.findServer(h.ServiceMethod)
	if err != nil {
		return req, err
	}
	// 新建两个入参实例，然后通过ReadBody将勤秋豹纹反序列化为第一个入参argv
	req.argv = req.mtype.newArgv()
	req.replyv = req.mtype.newReplyv()

	argvi := req.argv.Interface()
	// 检查是否是指针，不是是的话获得一个指针
	if req.argv.Type().Kind() != reflect.Ptr {
		argvi = req.argv.Addr().Interface()
	}
	if err = cc.ReadBody(argvi); err != nil {
		log.Println("rpc server: read body err：", err)
		return req, err
	}
	return req, nil
}

// 回复请求
func (server *Server) sendResponse(cc codec.Codec, h *codec.Header, body interface{}, sending *sync.Mutex) {
	sending.Lock()
	defer sending.Unlock()
	if err := cc.Write(h, body); err != nil {
		log.Println("rpc server: write response error:", err)
	}
}

// 处理请求
func (server *Server) handleRequest(cc codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup, timeout time.Duration) {
	defer wg.Done()
	called := make(chan struct{})
	sent := make(chan struct{})
	go func() {
		err := req.svc.call(req.mtype, req.argv, req.replyv)
		called <- struct{}{}
		if err != nil {
			req.h.Error = err.Error()
			server.sendResponse(cc, req.h, invalidRequest, sending)
			sent <- struct{}{}
			return
		}
		server.sendResponse(cc, req.h, req.replyv.Interface(), sending)
		sent <- struct{}{}
	}()
	if timeout == 0 {
		<-called
		<-sent
		return
	}
	select {
	case <-time.After(timeout):
		req.h.Error = fmt.Errorf("rpc server: request handle timeout within %s", timeout).Error()
		server.sendResponse(cc, req.h, invalidRequest, sending)
	case <-called:
		<-sent
	}

}

func (server *Server) Register(rcvr interface{}) error {
	s := newService(rcvr)
	// 保存这个键值，如果保存的键值已经存在，那么返回true
	if _, dup := server.serviceMap.LoadOrStore(s.name, s); dup {
		return errors.New("rpc: service already defind：" + s.name)
	}
	return nil
}

func Register(rcvr interface{}) error {
	return DefaultServer.Register(rcvr)
}

// 找到目标服务
func (server *Server) findServer(serviceMethod string) (svc *service, mtype *methodType, err error) {
	dot := strings.LastIndex(serviceMethod, ".")
	if dot < 0 {
		err = errors.New("rpc server：service/method request ill-formed：" + serviceMethod)
		return
	}
	serviceName, methodName := serviceMethod[:dot], serviceMethod[dot+1:]
	// 查看服务是否存在
	svci, ok := server.serviceMap.Load(serviceName)
	if !ok {
		err = errors.New("rpc server：can't find service" + serviceName)
	}
	svc = svci.(*service)
	mtype = svc.method[methodName]
	if mtype == nil {
		err = errors.New("rpc server：can't find method " + methodName)
	}
	return
}
