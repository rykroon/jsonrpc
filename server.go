package jsonrpc

import (
	"context"
	"net/rpc"
)

type JsonRpcServer interface {
	ServeJsonRpc(ctx context.Context, req *Request) *Response
}

type Server struct {
	server *rpc.Server
}

func NewServer() *Server {
	return &Server{
		server: rpc.NewServer(),
	}
}

func (s *Server) Register(rcvr any) {
	s.server.Register(rcvr)
}

func (s *Server) RegisterName(name string, rcvr any) {
	s.server.RegisterName(name, rcvr)
}

func (s *Server) ServeJsonRpc(ctx context.Context, req *Request) *Response {
	codec := newCodec(req)
	if req.IsNotification() {
		go s.server.ServeRequest(codec)
		return nil
	}

	s.server.ServeRequest(codec)
	return codec.resp
}
