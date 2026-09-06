package jsonrpc

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
)

// Raw adapts a Handler into a RawHandler: it decodes the raw params into P,
// calls fn, and marshals fn's result. opts are the json/v2 options for both
// the decode and the encode; Server.Register passes the server's, and none
// means json/v2's defaults.
//
// The decode owns structural failures only. Absent params yield the zero P
// without consulting any unmarshaler, and params json/v2 cannot fit into P —
// a wrong type, a malformed value, a duplicate name — are reported as Invalid
// params, the code the spec reserves for params that do not match what the
// method expects. Whether the decoded values are acceptable is fn's judgment,
// not the decoder's: fn returns its own error, and the server's ErrorHandler
// classifies it.
//
// Every other error passes through untouched — fn's error reaches the
// ErrorHandler as fn returned it, wrapping and all — as does a failure to
// marshal the result, which the ErrorHandler reports as an Internal error.
//
// The result is an ordinary value: reuse it, wrap it in Middleware to run
// logic before the decode, or install it with Server.RegisterRaw to give one
// method its own options.
func Raw[P, R any](fn Handler[P, R], opts ...json.Options) RawHandler {
	return func(ctx context.Context, raw jsontext.Value) (jsontext.Value, error) {
		var p P
		if len(raw) > 0 {
			if err := json.Unmarshal(raw, &p, opts...); err != nil {
				return nil, NewError(CodeInvalidParams, err.Error())
			}
		}
		r, err := fn(ctx, p)
		if err != nil {
			return nil, err
		}
		return json.Marshal(r, opts...)
	}
}
