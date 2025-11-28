package jsonrpc

import (
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

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var req *Request
	if err := decoder.Decode(&req); err != nil {
		switch err.(type) {
		case *json.SyntaxError:
			h.writeHttpError(w, nil, NewErrorTyped(ErrorCodeParseError, err.Error(), nil))
		default:
			h.writeHttpError(w, nil, NewErrorTyped(ErrorCodeInvalidRequest, err.Error(), nil))
		}
		return
	}

	fmt.Println("request: ", req.JsonRpc, req.Method, req.Id, req.Params)

	// validate Request
	if req.JsonRpc != "2.0" {
		h.writeHttpError(w, req.Id, NewErrorTyped(ErrorCodeInvalidRequest, "jsonrpc must be 2.0", nil))
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
		h.writeHttpError(w, req.Id, NewErrorTyped(
			ErrorCodeInternalError,
			fmt.Sprintf("failed to encode jsonrpc response as json: %w", err),
			nil,
		))
	}
}

func (h *HttpHandler) writeHttpError(w http.ResponseWriter, id *Id, err *Error) {
	resp := NewErrorResp(id, err)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
