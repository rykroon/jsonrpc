package jsonrpc

import (
	"context"
	"io"
	"net/rpc"
)

// the jsonrpc codec is responsible for reading data from
// a transport and making a jsonrpc Request
// as well as taking a jsonrpc Response and writing it
// to the transport
type JsonRpcCodec interface {
	ReadRequest(io.ReadCloser, *Request) error
	WriteResponse(io.WriteCloser, *Response) error
}

// type JsonRpcBatchCodec interface {
// 	ReadRequests(io.ReadCloser, []*Request) error
// 	WriteResponses(io.WriteCloser, []*Response) error
// }

type jsonRpcCodec struct{}

func (c *jsonRpcCodec) ReadRequest(r io.ReadCloser, req *Request) error {
	defer r.Close()
	// read body and validate jsonrpc request
	return nil
}

func (c *jsonRpcCodec) WriteResponse(w io.WriteCloser, resp *Request) error {
	defer w.Close()
	// write the jsonrpc response
	return nil
}

type JsonRpcServer2 interface {
	ServeJsonRpc(jsonRpcCodec) error
}

type JsonRpcServer interface {
	ServeJsonRpc(ctx context.Context, req *Request) *Response
}

type server struct {
	server *rpc.Server
}

func NewServer() *server {
	return &server{
		server: rpc.NewServer(),
	}
}

func (s *server) Register(rcvr any) {
	s.server.Register(rcvr)
}

func (s *server) RegisterName(name string, rcvr any) {
	s.server.RegisterName(name, rcvr)
}

func (s *server) ServeJsonRpc(ctx context.Context, req *Request) *Response {
	codec := newCodec(req)
	if req.IsNotification() {
		go s.server.ServeRequest(codec)
		return nil
	}

	s.server.ServeRequest(codec)
	return codec.resp
}
