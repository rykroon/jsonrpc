package jsonrpc

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
)

// Raw adapts a Handler into a RawHandler, decoding params into P and encoding
// R under opts. Absent params give the zero P; params that do not fit P are
// Invalid params. Other errors pass through. Use it with Server.RegisterRaw to
// give one method its own options.
func Raw[P, R any](fn Handler[P, R], opts ...json.Options) RawHandler {
	return func(ctx context.Context, req Request) (jsontext.Value, error) {
		var p P
		if err := req.Params().Decode(&p, opts...); err != nil {
			return nil, NewError(CodeInvalidParams, err.Error()).Err()
		}
		r, err := fn(ctx, p)
		if err != nil {
			return nil, err
		}
		return json.Marshal(r, opts...)
	}
}
