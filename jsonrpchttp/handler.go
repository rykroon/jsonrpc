// Package jsonrpchttp provides HTTP adapters for the jsonrpc package.
//
// Server side: Handler is an http.Handler that wraps a *jsonrpc.Server.
// Client side: Sender implements jsonrpc.Sender by POSTing requests to a URL.
//
// JSON-RPC errors — including parse errors — are returned in-band with
// HTTP 200; notifications produce 204 No Content. Transports that need
// HTTP-level parse failures should write their own handler that calls
// (*jsonrpc.Server).Serve directly.
package jsonrpchttp

import (
	"errors"
	"io"
	"mime"
	"net/http"

	"github.com/rykroon/jsonrpc"
)

// Handler adapts a *jsonrpc.Server to an http.Handler. JSON-RPC messages
// are accepted as POST request bodies; responses are written as JSON.
type Handler struct {
	// Server is the JSON-RPC server to dispatch to. Required.
	Server *jsonrpc.Server

	// MaxBodyBytes caps the request body size. Zero means no limit.
	MaxBodyBytes int64
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Compare the media type only, ignoring parameters (e.g. charset) and
	// case, so well-formed requests like "application/json; charset=utf-8"
	// are accepted. A missing or unparseable Content-Type is rejected.
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		http.Error(w, "unsupported media type", http.StatusUnsupportedMediaType)
		return
	}

	body := r.Body
	if h.MaxBodyBytes > 0 {
		body = http.MaxBytesReader(w, body, h.MaxBodyBytes)
	}
	data, err := io.ReadAll(body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}

	out, err := h.Server.ServeMessage(r.Context(), data)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if out == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(out)
}
