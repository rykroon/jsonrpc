package jsonrpc

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"sync"
	"sync/atomic"
)

// Sender round-trips a Request to a Response across a transport. The error
// return is for transport failures only; errors reported by the server appear
// in Response.Error. For notifications the Response is ignored and should be
// nil.
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

	mu   sync.Mutex
	opts json.Options
	used bool
}

func NewClient(sender Sender) *Client {
	return &Client{sender: sender}
}

// SetOptions installs json/v2 options for the params Call and Notify marshal
// and the result Call decodes, mirroring Server.SetOptions on the other side
// of the wire. Later calls replace earlier ones. Panics once Call or Notify
// has run, so neither can silently miss options set after it.
//
// Send is unaffected: its Request is already built, and the envelope is the
// Sender's to encode.
func (c *Client) SetOptions(opts ...json.Options) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.used {
		panic("jsonrpc: SetOptions must be called before Call or Notify")
	}
	c.opts = json.JoinOptions(opts...)
}

// options returns the client's options, closing them to further SetOptions.
func (c *Client) options() json.Options {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.used = true
	return c.opts
}

// Call invokes method with params (marshaled by NewParams) and decodes the
// reply into result; a nil result skips decoding. The id comes from an internal
// counter. Errors reported by the server are returned as *Error; any other
// error is a transport or decode failure.
func (c *Client) Call(ctx context.Context, method string, params any, result any) error {
	opts := c.options()
	raw, err := NewParams(params, opts)
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
	// A Sender is free to build a Response itself, so guard the shape the spec
	// forbids: a response carrying neither member is a failure Call cannot
	// name, not a success whose result happens to be empty.
	if len(resp.Result) == 0 {
		return fmt.Errorf("jsonrpc: response for call %q carried neither result nor error", method)
	}
	return resp.Decode(result, opts)
}

// Notify sends a notification: the server dispatches method but produces no
// response. The error reports transport failures only.
func (c *Client) Notify(ctx context.Context, method string, params any) error {
	raw, err := NewParams(params, c.options())
	if err != nil {
		return fmt.Errorf("jsonrpc: marshal params: %w", err)
	}
	_, err = c.sender.Send(ctx, NewNotification(method, raw))
	return err
}

// Send round-trips req via the underlying Sender. Server-reported errors appear
// in Response.Error, not in the error return; for a notification the Sender's
// response is returned as-is (typically nil).
func (c *Client) Send(ctx context.Context, req *Request) (*Response, error) {
	return c.sender.Send(ctx, req)
}
