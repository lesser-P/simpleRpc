package codec

import "io"

// 请求和响应中的参数和返回值抽象为body，剩余的信息放在header中，那么就可以抽象出数据结构Header：
type Header struct {
	ServiceMethod string //服务名和方法名
	Seq           uint64 //请求的序列号
	Error         string //错误信息
}

// 实现不同的Codec实例，抽象出对消息体进行编解码的接口Codec：
type Codec interface {
	io.Closer
	ReadHeader(header *Header) error
	ReadBody(interface{}) error
	Write(*Header, interface{}) error
}

type NewCodecFunc func(io.ReadWriteCloser) Codec

type Type string

const (
	GobType  Type = "application/gob"
	JsonType Type = "application/json"
)

var NewCodecFuncMap map[Type]NewCodecFunc

func init() {
	NewCodecFuncMap = make(map[Type]NewCodecFunc)
	NewCodecFuncMap[GobType] = NewGobCodec
}
