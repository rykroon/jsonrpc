package jsonrpc

import (
	"bytes"
	"encoding/json"
	"errors"
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
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)

	// add logic for batched requests.
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	req := &Request{}
	if err := decoder.Decode(&req); err != nil {
		var (
			syntaxErr *json.SyntaxError
			typeErr   *json.UnmarshalTypeError
		)
		var jsonRpcErr *Error
		if errors.As(err, &syntaxErr) {
			jsonRpcErr = &Error{Code: ErrorCodeParseError, Message: err.Error()}
		} else if errors.As(err, &typeErr) {
			jsonRpcErr = &Error{Code: ErrorCodeInvalidRequest, Message: err.Error()}
		} else {
			jsonRpcErr = &Error{Code: ErrorCodeInternalError, Message: err.Error()}
		}
		h.writeResponse(w, NewErrorResp(NullId(), jsonRpcErr))
		return
	}

	// validate Request
	if req.JsonRpc != "2.0" {
		err := &Error{Code: ErrorCodeInvalidRequest, Message: "jsonrpc must be 2.0"}
		h.writeResponse(w, NewErrorResp(req.Id, err))
		return
	}

	if req.Method == "" {
		err := &Error{Code: ErrorCodeInvalidRequest, Message: "missing method"}
		h.writeResponse(w, NewErrorResp(req.Id, err))
	}

	if req.IsNotification() {
		go h.Server.ServeJsonRpc(r.Context(), req)
		w.WriteHeader(http.StatusNoContent)
		return
	}

	resp := h.Server.ServeJsonRpc(r.Context(), req)
	err := h.writeResponse(w, resp)

	if err != nil {
		err := &Error{
			Code:    ErrorCodeInternalError,
			Message: fmt.Sprintf("failed to encode jsonrpc response as json: %s", err.Error()),
		}
		h.writeResponse(w, NewErrorResp(req.Id, err))
	}
}

func (h *HttpHandler) writeResponse(w http.ResponseWriter, resp *Response) error {
	w.Header().Set("Content-Type", "application/json")
	return json.NewEncoder(w).Encode(resp)
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
