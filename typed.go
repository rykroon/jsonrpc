package jsonrpc

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
)

// DecodeParams unmarshals raw into a fresh P; empty raw returns the zero P
// with no error, without consulting any unmarshaler. Failure is reported as
// CodeInvalidParams, with the cause as the message.
//
// Decoding follows encoding/json/v2 semantics under opts: by default
// duplicate names and invalid UTF-8 are rejected, names match fields
// case-sensitively, and unknown members are tolerated — validate in
// Middleware for strict params. Install a json.UnmarshalFromFunc for P via
// json.WithUnmarshalers to take over the decode entirely.
func DecodeParams[P any](raw jsontext.Value, opts ...json.Options) (P, *Error) {
	var p P
	if len(raw) == 0 {
		return p, nil
	}
	if err := json.Unmarshal(raw, &p, opts...); err != nil {
		return p, NewError(CodeInvalidParams, err.Error())
	}
	return p, nil
}

// MarshalResult marshals v under opts, reporting failure as CodeInternalError
// with the cause as the message. A json.MarshalToFunc for v's type, installed
// via json.WithMarshalers, controls its wire form.
func MarshalResult(v any, opts ...json.Options) (jsontext.Value, *Error) {
	out, err := json.Marshal(v, opts...)
	if err != nil {
		return nil, NewError(CodeInternalError, err.Error())
	}
	return out, nil
}

// Raw adapts a Handler into a RawHandler that decodes raw into P with
// DecodeParams, calls fn, converts its error (preserving *Error), and marshals
// the result with MarshalResult. opts are the json/v2 options for both the
// decode and the encode; Server.Register passes the server's, and none means
// json/v2's defaults.
//
// The result is an ordinary value: reuse it, wrap it in Middleware to run
// logic before the decode, or install it with Server.RegisterRaw to give one
// method its own options.
func Raw[P, R any](fn Handler[P, R], opts ...json.Options) RawHandler {
	return func(ctx context.Context, raw jsontext.Value) (jsontext.Value, *Error) {
		p, rpcErr := DecodeParams[P](raw, opts...)
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
		return MarshalResult(r, opts...)
	}
}
