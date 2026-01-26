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
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if r.Header.Get("Content-Type") != "application/json" {
		http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
		return
	}

	req, err := DefaultRequestdecoder(r.Body)
	if err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return
	}

	resp := h.Server.ServeJsonRpc(req)
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
		)
		h.writeResponse(w, NewErrorResponse(err, req.Id()))
	}
}

func (h *HttpHandler) writeResponse(w http.ResponseWriter, resp Response) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
