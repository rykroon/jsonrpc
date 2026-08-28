package jsonrpc

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
)

// DecodeParams unmarshals raw into a fresh P; empty raw returns the zero P
// with no error. Failure is reported as CodeInvalidParams, with the cause as
// the message.
//
// Decoding follows encoding/json/v2 semantics: duplicate names and invalid
// UTF-8 are rejected, names match fields case-sensitively, and unknown
// members are tolerated — validate in Middleware for strict params.
func DecodeParams[P any](raw jsontext.Value) (P, *Error) {
	var p P
	if len(raw) == 0 {
		return p, nil
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return p, NewError(CodeInvalidParams, err.Error())
	}
	return p, nil
}

// MarshalResult marshals v, reporting failure as CodeInternalError with the
// cause as the message.
func MarshalResult(v any) (jsontext.Value, *Error) {
	out, err := json.Marshal(v)
	if err != nil {
		return nil, NewError(CodeInternalError, err.Error())
	}
	return out, nil
}

// Typed adapts a typed function into a Handler that decodes raw into P, calls
// fn, converts its error (preserving *Error), and marshals the result. The
// result is an ordinary value: reuse it, or wrap it in Middleware to run
// logic before the decode.
func Typed[P, R any](fn TypedHandler[P, R]) Handler {
	return func(ctx context.Context, raw jsontext.Value) (jsontext.Value, *Error) {
		p, rpcErr := DecodeParams[P](raw)
		if rpcErr != nil {
			return nil, rpcErr
		}
		r, err := fn(ctx, p)
		if err != nil {
			if e, ok := errors.AsType[*Error](err); ok {
				// A typed-nil *Error inside a non-nil error must not read as
				// success; calling err.Error() on it would also panic.
				if e == nil {
					return nil, NewError(CodeInternalError, "handler returned a nil *jsonrpc.Error")
				}
				return nil, e
			}
			return nil, NewError(CodeInternalError, err.Error())
		}
		return MarshalResult(r)
	}
}
