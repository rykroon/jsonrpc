package jsonrpc

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
)

// Raw adapts a Handler into a RawHandler: params into P, fn, result out. opts
// apply to both sides; Server.Register passes the server's. Absent params
// yield the zero P without consulting any unmarshaler; params that do not fit
// P are Invalid params. Every other error passes through untouched, as does a
// failed result marshal. Install the result with Server.RegisterRaw to give
// one method its own options.
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
