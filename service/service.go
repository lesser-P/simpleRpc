package service

import (
	"go/ast"
	"log"
	"reflect"
	"sync/atomic"
)

type methodType struct {
	method    reflect.Method
	ArgType   reflect.Type
	ReplyType reflect.Type
	numCalls  uint64
}

func (m *methodType) NumCalls() uint64 {
	// atomic.LoadUint64 函数可以安全地读取 m.numCalls 的值
	return atomic.LoadUint64(&m.numCalls)
}

func (m *methodType) newArgv() reflect.Value {
	var argv reflect.Value

	// 如果参数是指针类型，那么直接创建一个新的实例
	if m.ArgType.Kind() == reflect.Ptr {
		argv = reflect.New(m.ArgType.Elem())
	} else {
		argv = reflect.New(m.ArgType).Elem()
	}
	return argv
}

func (m *methodType) newReplyv() reflect.Value {
	replyv := reflect.New(m.ReplyType.Elem())
	// 选择指向的类型种类
	switch m.ReplyType.Elem().Kind() {
	case reflect.Map:
		replyv.Elem().Set(reflect.MakeMap(m.ReplyType.Elem()))
	case reflect.Slice:
		replyv.Elem().Set(reflect.MakeSlice(m.ReplyType.Elem(), 0, 0))
	}
	return replyv
}

type service struct {
	name   string
	typ    reflect.Type
	rcvr   reflect.Value
	method map[string]*methodType
}

func newService(rcvr interface{}) *service {
	s := new(service)

	s.rcvr = reflect.ValueOf(rcvr)
	// 简化指针和非指针类型的处理
	s.name = reflect.Indirect(s.rcvr).Type().Name()
	s.typ = reflect.TypeOf(rcvr)
	if !ast.IsExported(s.name) {
		log.Fatalf("rpc server: %s is not a valid service name", s.name)
	}
	s.registerMethods()
	return s
}

// 通过反射检查并注册那些符合特定签名要求的方法，使得这些方法可以在之后被远程调用。
func (s *service) registerMethods() {
	s.method = make(map[string]*methodType)
	// 遍历该结构的方法
	for i := 0; i < s.typ.NumMethod(); i++ {
		method := s.typ.Method(i)
		mType := method.Type
		//这段代码检查当前遍历到的方法是否具有期望的签名。首先，它确认方法有三个输入参数（包括接收者）和一个输出参数。
		//然后，它检查唯一的输出参数是否为error类型。如果任一条件不满足，循环会通过continue跳过当前迭代。
		if mType.NumIn() != 3 || mType.NumOut() != 1 {
			continue
		}
		if mType.Out(0) != reflect.TypeOf((*error)(nil)).Elem() {
			continue
		}

		argType, replyType := mType.In(1), mType.In(2)
		if !isExportdOrBuiltinType(argType) || !isExportdOrBuiltinType(replyType) {
			continue
		}
		// 在这里注册方法
		s.method[method.Name] = &methodType{
			method:    method,
			ArgType:   argType,
			ReplyType: replyType,
		}
		log.Printf("rpc server: register %s.%s\n", s.name, method.Name)
	}
}

func isExportdOrBuiltinType(t reflect.Type) bool {
	return ast.IsExported(t.Name()) || t.PkgPath() == ""
}

func (s *service) call(m *methodType, argv, replyv reflect.Value) error {
	// 原子操作+1
	atomic.AddUint64(&m.numCalls, 1)
	// 实际要调用的那个方法
	f := m.method.Func
	// 多个参数就构建切片
	returnValue := f.Call([]reflect.Value{s.rcvr, argv, replyv})
	if errInter := returnValue[0].Interface(); errInter != nil {
		return errInter.(error)
	}
	return nil
}
