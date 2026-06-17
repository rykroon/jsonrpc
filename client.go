package jsonrpc

import (
	"context"
	"encoding/json"
	"strconv"
	"sync/atomic"
)

// Sender round-trips a Request to a Response across a transport. The error
// return is for transport failures (network error, framing, etc.) that occur
// before a JSON-RPC reply is produced. Errors reported by the server appear
// inside Response.Error, not as the returned error.
//
// For notifications, Sender is invoked with req.IsNotification() == true; the
// returned *Response is ignored (transports may return nil).
type Sender func(ctx context.Context, req *Request) (*Response, error)

// InProcess adapts a Server into a Sender, suitable for passing to NewClient
// when client and server live in the same process. Serve cannot fail, so the
// returned Sender's error is always nil.
func InProcess(s *Server) Sender {
	return func(ctx context.Context, req *Request) (*Response, error) {
		return s.Serve(ctx, req), nil
	}
}

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
// A non-nil error is either a marshal/unmarshal failure, a transport error
// from the Sender, or a *jsonrpc.Error returned by the server.
func (c *Client) Call(ctx context.Context, method string, params, out any) error {
	p, err := marshalParams(params)
	if err != nil {
		return err
	}
	id := json.RawMessage(strconv.FormatUint(c.next.Add(1), 10))
	resp, err := c.send(ctx, &Request{JSONRPC: Version, Method: method, Params: p, ID: id})
	if err != nil {
		return err
	}
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

// Notify sends a notification (no ID, no response expected). The Sender may
// still return a transport error, which is propagated.
func (c *Client) Notify(ctx context.Context, method string, params any) error {
	p, err := marshalParams(params)
	if err != nil {
		return err
	}
	_, err = c.send(ctx, &Request{JSONRPC: Version, Method: method, Params: p})
	return err
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
