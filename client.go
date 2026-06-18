package jsonrpc

import "context"

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

// Client wraps a Sender. Build a *Request with NewRequest or NewNotification
// (and NewID / NewParams for the polymorphic fields) and round-trip it with
// Send.
type Client struct {
	send Sender
}

func NewClient(send Sender) *Client {
	return &Client{send: send}
}

// Send round-trips req via the underlying Sender. Transport failures are
// returned as the error; JSON-RPC errors from the server appear inside
// Response.Error. For notifications the Sender's response is returned as-is
// (typically nil).
func (c *Client) Send(ctx context.Context, req *Request) (*Response, error) {
	return c.send(ctx, req)
}
