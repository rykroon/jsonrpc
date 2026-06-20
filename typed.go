package jsonrpc

import (
	"context"
	"encoding/json"
	"errors"
)

// DecodeParams unmarshals raw into a fresh P. An empty raw returns the zero
// value of P with no error (the spec allows omitting params). Unmarshal
// failure is reported as CodeInvalidParams.
func DecodeParams[P any](raw json.RawMessage) (P, *Error) {
	var p P
	if len(raw) == 0 {
		return p, nil
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, NewError(CodeInvalidParams, err.Error())
	}
	return p, nil
}

// MarshalResult marshals v into JSON bytes. Marshal failure is reported as
// CodeInternalError.
func MarshalResult(v any) (json.RawMessage, *Error) {
	out, err := json.Marshal(v)
	if err != nil {
		return nil, NewError(CodeInternalError, err.Error())
	}
	return out, nil
}

// Typed adapts a typed function into a HandlerFunc, running the full typed
// pipeline on each call: decode raw into P, call fn, convert its error
// (preserving *Error via errors.As), marshal the result.
//
// The returned HandlerFunc is an ordinary value: register it, wrap it in
// Middleware, or reuse it under several names. To run logic before the decode
// (auth, JSON schema validation, etc.), wrap Typed(fn) in Middleware rather
// than hand-wiring the pipeline.
func Typed[P, R any](fn func(context.Context, P) (R, error)) HandlerFunc {
	return func(ctx context.Context, raw json.RawMessage) (json.RawMessage, *Error) {
		p, rpcErr := DecodeParams[P](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		r, err := fn(ctx, p)
		if err != nil {
			var e *Error
			if errors.As(err, &e) {
				return nil, e
			}
			return nil, NewError(CodeInternalError, err.Error())
		}
		return MarshalResult(r)
	}
}

// Register adapts a typed function into a HandlerFunc with Typed and installs
// it on s, wrapped with the given per-method middleware (mw[0] outermost)
// followed by the server-wide middleware.
//
// Equivalent to s.RegisterHandler(name, Typed(fn), mw...).
func Register[P, R any](s *Server, name string, fn func(context.Context, P) (R, error), mw ...Middleware) {
	s.RegisterHandler(name, Typed(fn), mw...)
}
