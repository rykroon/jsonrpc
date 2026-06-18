package jsonrpc

import "context"

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

// Client wraps a Sender. Build a *Request with NewRequest or NewNotification
// (and NewID / NewParams for the polymorphic fields) and round-trip it with
// Send.
type Client struct {
	sender Sender
}

func NewClient(sender Sender) *Client {
	return &Client{sender: sender}
}

// Send round-trips req via the underlying Sender. Transport failures are
// returned as the error; JSON-RPC errors from the server appear inside
// Response.Error. For notifications the Sender's response is returned as-is
// (typically nil).
func (c *Client) Send(ctx context.Context, req *Request) (*Response, error) {
	return c.sender.Send(ctx, req)
}
