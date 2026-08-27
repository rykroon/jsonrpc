package jsonrpc

import (
	"context"
	"fmt"
	"sync/atomic"
)

// Sender round-trips a Request to a Response across a transport. The error
// return is for transport failures only; errors reported by the server appear
// in Response.Error. For notifications the returned *Response is ignored and
// may be nil.
type Sender interface {
	Send(ctx context.Context, req *Request) (*Response, error)
}

// SenderFunc adapts a plain function into a Sender, mirroring net/http's
// Handler / HandlerFunc.
type SenderFunc func(ctx context.Context, req *Request) (*Response, error)

func (f SenderFunc) Send(ctx context.Context, req *Request) (*Response, error) {
	return f(ctx, req)
}

// Sender returns a Sender that dispatches directly to s, for a client and
// server in the same process. Its error is always nil.
func (s *Server) Sender() Sender {
	return SenderFunc(func(ctx context.Context, req *Request) (*Response, error) {
		return s.Serve(ctx, req), nil
	})
}

// Client wraps a Sender. Call and Notify marshal params, generate ids, and
// decode results; Send is the escape hatch for pre-built *Request values.
type Client struct {
	sender Sender
	nextID atomic.Int64
}

func NewClient(sender Sender) *Client {
	return &Client{sender: sender}
}

// Call invokes method with params (marshaled by NewParams) and decodes the
// result into result; a nil result skips decoding. The id comes from an
// internal counter. Errors reported by the server are returned as *Error;
// any other error is a transport or decode failure.
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
// response. The error reports transport failures only.
func (c *Client) Notify(ctx context.Context, method string, params any) error {
	raw, err := NewParams(params)
	if err != nil {
		return fmt.Errorf("jsonrpc: marshal params: %w", err)
	}
	_, err = c.sender.Send(ctx, NewNotification(method, raw))
	return err
}

// Send round-trips req via the underlying Sender. JSON-RPC errors from the
// server appear in Response.Error, not the error return; for notifications
// the Sender's response is returned as-is (typically nil).
func (c *Client) Send(ctx context.Context, req *Request) (*Response, error) {
	return c.sender.Send(ctx, req)
}
