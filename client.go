package jsonrpc

import (
	"context"
	"encoding/json"
	"strconv"
	"sync/atomic"
)

// Sender round-trips a Request to a Response. For notifications it is called
// with req.IsNotification() == true and the returned *Response is ignored.
//
// Implementations may be a *Mux (in-process), an HTTP client, or any other
// transport. Wiring is left to the caller.
type Sender func(ctx context.Context, req *Request) *Response

// Client is a convenience wrapper that marshals params, generates IDs, and
// decodes results. It holds no state beyond the ID counter.
type Client struct {
	send Sender
	next atomic.Uint64
}

func NewClient(send Sender) *Client {
	return &Client{send: send}
}

// Call invokes method with params and decodes the result into out. Pass nil
// for params or out to skip marshaling/unmarshaling the respective side.
// A non-nil error is either a marshal/unmarshal failure or a *jsonrpc.Error
// returned by the server.
func (c *Client) Call(ctx context.Context, method string, params, out any) error {
	p, err := marshalParams(params)
	if err != nil {
		return err
	}
	id := json.RawMessage(strconv.FormatUint(c.next.Add(1), 10))
	resp := c.send(ctx, &Request{JSONRPC: Version, Method: method, Params: p, ID: id})
	if resp == nil {
		return NewError(CodeInternalError, "nil response")
	}
	if resp.Error != nil {
		return resp.Error
	}
	if out != nil && len(resp.Result) > 0 {
		return json.Unmarshal(resp.Result, out)
	}
	return nil
}

// Notify sends a notification (no ID, no response expected).
func (c *Client) Notify(ctx context.Context, method string, params any) error {
	p, err := marshalParams(params)
	if err != nil {
		return err
	}
	c.send(ctx, &Request{JSONRPC: Version, Method: method, Params: p})
	return nil
}

func marshalParams(params any) (json.RawMessage, error) {
	if params == nil {
		return nil, nil
	}
	if raw, ok := params.(json.RawMessage); ok {
		return raw, nil
	}
	return json.Marshal(params)
}
