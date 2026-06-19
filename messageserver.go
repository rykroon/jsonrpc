package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"
)

// MessageServer is the byte-level entry point for transports that work in
// raw JSON messages (WebSocket, stdio, TCP). It parses a message into a
// Request, dispatches it via the wrapped Server, and marshals the Response.
//
// Currently single requests only — batch messages (JSON arrays) are
// rejected with CodeInvalidRequest. Batch support is planned.
type MessageServer struct {
	// Server is the underlying request dispatcher. Required.
	Server *Server
}

// ServeMessage parses data as a JSON-RPC message, dispatches it via the
// wrapped Server, and returns the marshaled response bytes. Notifications
// produce (nil, nil) — there is no reply to send.
//
// JSON-RPC errors (parse errors, invalid request, etc.) are returned
// in-band as a marshaled error Response, not as the error return. The
// error return is reserved for response marshaling failures, which should
// not occur in normal operation.
func (m *MessageServer) ServeMessage(ctx context.Context, data json.RawMessage) (json.RawMessage, error) {
	if isJSONArray(data) {
		return marshalMessageError(NewError(CodeInvalidRequest, "batch requests are not supported"))
	}
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		var syntaxErr *json.SyntaxError
		if errors.As(err, &syntaxErr) {
			return marshalMessageError(NewError(CodeParseError, err.Error()))
		}
		return marshalMessageError(NewError(CodeInvalidRequest, err.Error()))
	}
	resp := m.Server.Serve(ctx, &req)
	if resp == nil {
		return nil, nil
	}
	return json.Marshal(resp)
}

func isJSONArray(data []byte) bool {
	for _, b := range data {
		if b == ' ' || b == '\t' || b == '\r' || b == '\n' {
			continue
		}
		return b == '['
	}
	return false
}

func marshalMessageError(e *Error) (json.RawMessage, error) {
	return json.Marshal(&Response{
		JSONRPC: Version,
		Error:   e,
		ID:      json.RawMessage("null"),
	})
}
