package jsonrpc

import (
	"context"
	"fmt"
	"sync/atomic"
)

// Sender round-trips a Request to a Response across a transport. The error
// return is for transport failures (network error, framing, etc.) that occur
// before a JSON-RPC reply is produced. Errors reported by the server appear
// inside Response.Error, not as the returned error.
//
// For notifications, Send is invoked with req.IsNotification() == true; the
// returned *Response is ignored (transports may return nil).
type Sender interface {
	Send(ctx context.Context, req *Request) (*Response, error)
}

// SenderFunc adapts a plain function into a Sender. The pattern mirrors
// net/http's Handler / HandlerFunc.
type SenderFunc func(ctx context.Context, req *Request) (*Response, error)

func (f SenderFunc) Send(ctx context.Context, req *Request) (*Response, error) {
	return f(ctx, req)
}

// InProcess adapts a Server into a Sender, suitable for passing to NewClient
// when client and server live in the same process. Serve cannot fail, so the
// returned Sender's error is always nil.
func InProcess(s *Server) Sender {
	return SenderFunc(func(ctx context.Context, req *Request) (*Response, error) {
		return s.Serve(ctx, req), nil
	})
}

// Client wraps a Sender. Call and Notify are the convenience path: they
// marshal params, generate ids, and decode results. Send is the low-level
// escape hatch for pre-built *Request values (custom ids, raw params).
type Client struct {
	sender Sender
	nextID atomic.Int64
}

func NewClient(sender Sender) *Client {
	return &Client{sender: sender}
}

// Call invokes method with params, decoding the result into result. Params
// are marshaled with NewParams (nil means no params; a json.RawMessage passes
// through). The request id is generated from an internal counter. A nil
// result skips decoding.
//
// Errors reported by the server are returned as *Error — recover the code
// and data with errors.As. Any other error is a transport or decode failure.
func (c *Client) Call(ctx context.Context, method string, params any, result any) error {
	raw, err := NewParams(params)
	if err != nil {
		return fmt.Errorf("jsonrpc: marshal params: %w", err)
	}
	resp, err := c.sender.Send(ctx, NewRequest(method, raw, NewID(c.nextID.Add(1))))
	if err != nil {
		return err
	}
	if resp == nil {
		return fmt.Errorf("jsonrpc: transport returned no response for call %q", method)
	}
	if resp.Error != nil {
		return resp.Error
	}
	return resp.Decode(result)
}

// Notify sends a notification: the server dispatches method but produces no
// response. The returned error reports transport failures only.
func (c *Client) Notify(ctx context.Context, method string, params any) error {
	raw, err := NewParams(params)
	if err != nil {
		return fmt.Errorf("jsonrpc: marshal params: %w", err)
	}
	_, err = c.sender.Send(ctx, NewNotification(method, raw))
	return err
}

// Send round-trips req via the underlying Sender. Transport failures are
// returned as the error; JSON-RPC errors from the server appear inside
// Response.Error. For notifications the Sender's response is returned as-is
// (typically nil).
func (c *Client) Send(ctx context.Context, req *Request) (*Response, error) {
	return c.sender.Send(ctx, req)
}
