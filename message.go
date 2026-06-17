package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"
)

// HandleMessage parses a JSON-RPC message from data, dispatches it via fn,
// and returns the marshaled response bytes. Use this from transport adapters
// that work in raw messages (WebSocket, stdio, TCP) and want the spec's
// in-band error handling.
//
// Currently single requests only — batch messages (JSON arrays) are rejected
// with CodeInvalidRequest.
//
// Returns (nil, nil) when the message is a notification. fn is typically
// (*Server).Serve, optionally wrapped for auth, logging, ctx adjustments, etc.
func HandleMessage(
	ctx context.Context,
	data []byte,
	fn func(context.Context, *Request) *Response,
) ([]byte, error) {
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
	resp := fn(ctx, &req)
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

func marshalMessageError(e *Error) ([]byte, error) {
	return json.Marshal(&Response{
		JSONRPC: Version,
		Error:   e,
		ID:      json.RawMessage("null"),
	})
}
