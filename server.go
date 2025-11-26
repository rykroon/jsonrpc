package jsonrpc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type JsonRpcHandler interface {
	ServeJsonRpc(ctx context.Context, req *Request) *Response
}

type HandlerFunc func(ctx context.Context, params Params) (any, error)

type JsonRpcServer struct {
	methods map[string]HandlerFunc
}

func NewJsonRpcServer() *JsonRpcServer {
	return &JsonRpcServer{
		methods: make(map[string]HandlerFunc),
	}
}

func (s *JsonRpcServer) Register(method string, handler HandlerFunc) {
	s.methods[method] = handler
}

func (s *JsonRpcServer) ServeJsonRpc(ctx context.Context, req *Request) *Response {
	handler, exists := s.methods[req.Method]
	if !exists {
		return NewErrorResp(req.IdForResponse(), MethodNotFound(req.Method))
	}

	if req.IsNotification() {
		go handler(ctx, req.Params)
		return nil
	}

	result, err := handler(ctx, req.Params)
	if err != nil {
		switch e := err.(type) {
		case *Error:
			return NewErrorResp(req.IdForResponse(), e)
		default:
			return NewErrorResp(req.IdForResponse(), InternalError(e.Error()))
		}
	}

	rawResult, err := json.Marshal(result)
	if err != nil {
		return NewErrorResp(req.IdForResponse(), InternalError(err.Error()))
	}
	return NewSuccessResp(req.IdForResponse(), rawResult)
}

func (s *JsonRpcServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	if r.Method != http.MethodPost {
		http.Error(w, "Only POST allowed", http.StatusMethodNotAllowed)
		return
	}

	if r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "Invalid Content-Type", http.StatusUnsupportedMediaType)
		return
	}

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var req *Request
	if err := decoder.Decode(&req); err != nil {
		switch err.(type) {
		case *json.SyntaxError:
			s.writeHttpError(w, NullId(), ParseError(err.Error()))
		default:
			s.writeHttpError(w, NullId(), InvalidRequest(err.Error()))
		}
		return
	}

	fmt.Println("request: ", req.Jsonrpc, req.Method, req.Id, req.Params)

	// validate Request
	if req.Jsonrpc != "2.0" {
		s.writeHttpError(w, req.IdForResponse(), InvalidRequest("jsonrpc must be 2.0"))
		return
	}

	if !req.Id.IsValidForRequest() {
		s.writeHttpError(w, req.IdForResponse(), InvalidRequest("invalid id"))
		return
	}

	resp := s.ServeJsonRpc(r.Context(), req)
	if resp == nil {
		// a notification
		w.WriteHeader(http.StatusNoContent)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.writeHttpError(w, req.Id, InternalError(
			fmt.Errorf("failed to encode jsonrpc response as json: %w", err),
		))
	}
}

func (s *JsonRpcServer) writeHttpError(w http.ResponseWriter, id Id, err error) {
	resp := NewErrorResp(id, err)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
