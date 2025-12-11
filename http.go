package jsonrpc

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

// Implements the http.Handle interface
type HttpHandler struct {
	Server JsonRpcServer
}

func NewHttpHandler(server JsonRpcServer) *HttpHandler {
	return &HttpHandler{Server: server}
}

func (h *HttpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()

	if r.Method != http.MethodPost {
		http.Error(w, "Only POST allowed", http.StatusMethodNotAllowed)
		return
	}

	if r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "Invalid Content-Type", http.StatusUnsupportedMediaType)
		return
	}

	// add logic for batched requests.
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	req := &Request{}
	if err := decoder.Decode(&req); err != nil {
		switch concreteErr := err.(type) {
		case *json.SyntaxError:
			err := NewError(ErrorCodeParseError, err.Error(), nil).(*Error)
			h.writeResponse(w, NewErrorResp(NullId(), err))
		case *Error:
			h.writeResponse(w, NewErrorResp(NullId(), concreteErr))
		default:
			err := NewError(ErrorCodeInvalidRequest, err.Error(), nil).(*Error)
			h.writeResponse(w, NewErrorResp(NullId(), err))
		}
		return
	}

	// validate Request
	if req.JsonRpc != "2.0" {
		err := NewError(ErrorCodeInvalidRequest, "jsonrpc must be 2.0", nil).(*Error)
		h.writeResponse(w, NewErrorResp(req.Id, err))
		return
	}

	resp := h.Server.ServeJsonRpc(r.Context(), req)
	if resp == nil {
		// a notification
		w.WriteHeader(http.StatusNoContent)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		err := NewError(
			ErrorCodeInternalError,
			fmt.Sprintf("failed to encode jsonrpc response as json: %s", err.Error()),
			nil,
		).(*Error)
		h.writeResponse(w, NewErrorResp(req.Id, err))
	}
}

func (h *HttpHandler) writeResponse(w http.ResponseWriter, resp *Response) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func NewHttpRequest(url string, req *Request) (*http.Request, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal json: %w", err)
	}

	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("failed to create http request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	return httpReq, nil
}
