package jsonrpc

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

// Sender round-trips a Request to a Response across a transport. The error
// return is for transport failures only; errors reported by the server appear
// in Response.Error. For notifications the Response is ignored and should be
// zero.
type Sender interface {
	Send(ctx context.Context, req Request) (Response, error)
}

// SenderFunc adapts a plain function into a Sender, mirroring net/http's
// Handler / HandlerFunc.
type SenderFunc func(ctx context.Context, req Request) (Response, error)

func (f SenderFunc) Send(ctx context.Context, req Request) (Response, error) {
	return f(ctx, req)
}

// Sender returns a Sender that dispatches directly to s, for a client and
// server in the same process. Its error is always nil.
func (s *Server) Sender() Sender {
	return SenderFunc(func(ctx context.Context, req Request) (Response, error) {
		return s.Serve(ctx, req), nil
	})
}

// Client wraps a Sender. Call and Notify marshal params, generate ids, and
// decode results; Send is the escape hatch for pre-built Request values.
type Client struct {
	sender Sender

	mu    sync.Mutex
	opts  json.Options
	idGen func() ID
	used  bool
}

func NewClient(sender Sender) *Client {
	return &Client{sender: sender, idGen: counterIDs()}
}

// counterIDs returns a generator of the ids 1, 2, 3, ..., safe for concurrent
// use.
func counterIDs() func() ID {
	var n atomic.Int64
	return func() ID { return IntID(n.Add(1)) }
}

// SetOptions installs json/v2 options for the params Call and Notify marshal
// and the result Call decodes, mirroring Server.SetOptions. Later calls replace
// earlier ones. Panics once Call or Notify has run. Send is unaffected, as its
// Request is already built.
func (c *Client) SetOptions(opts ...json.Options) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.used {
		panic("jsonrpc: SetOptions must be called before Call or Notify")
	}
	c.opts = json.JoinOptions(opts...)
}

// SetIDGenerator installs gen as the source of the ids Call sends, in place of
// the default counter (1, 2, 3, ...). gen must be safe for concurrent use if
// Call is, and should return a StringID or IntID that is unique among the
// client's in-flight calls; Call fails on a zero, null, or fractional id. A nil
// gen restores the counter. Panics once Call or Notify has run.
func (c *Client) SetIDGenerator(gen func() ID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.used {
		panic("jsonrpc: SetIDGenerator must be called before Call or Notify")
	}
	if gen == nil {
		gen = counterIDs()
	}
	c.idGen = gen
}

// config returns the client's options and id generator, closing them to
// further SetOptions and SetIDGenerator.
func (c *Client) config() (json.Options, func() ID) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.used = true
	return c.opts, c.idGen
}

// errEmptyMethod is what Call and Notify return rather than send a request
// every server must reject.
var errEmptyMethod = errors.New("jsonrpc: method must not be empty")

// newID returns the next id from gen, checking it is one a client may send.
func newID(gen func() ID) (ID, error) {
	id := gen()
	if !id.isClientID() {
		if id.IsZero() {
			return ID{}, errors.New("jsonrpc: id generator returned a zero ID")
		}
		return ID{}, fmt.Errorf("jsonrpc: id generator returned %s; want a string or integer", id)
	}
	return id, nil
}

// Call sends method with params (marshaled by NewParams) and decodes the reply
// into result, unless result is nil. It returns the id it sent, zero if nothing
// was sent. An empty method or a response with a different id is an error.
// A server-reported error is returned as an error carrying the Error; recover
// it with FromError. Other errors are transport or decode failures.
func (c *Client) Call(ctx context.Context, method string, params any, result any) (ID, error) {
	opts, gen := c.config()
	if method == "" {
		return ID{}, errEmptyMethod
	}
	p, err := NewParams(params, opts)
	if err != nil {
		return ID{}, err
	}
	id, err := newID(gen)
	if err != nil {
		return ID{}, err
	}
	resp, err := c.sender.Send(ctx, NewRequest(method, p, id))
	if err != nil {
		return id, err
	}
	if resp.IsZero() {
		return id, fmt.Errorf("jsonrpc: transport returned no response for call %q", method)
	}
	// Another call's result must not decode as this one's. A null id is
	// allowed on an error: the server could not read ours.
	rpcErr, isErr := resp.Error()
	if resp.ID() != id && !(resp.ID().IsNull() && isErr) {
		return id, fmt.Errorf("jsonrpc: response id %s does not match request id %s for call %q", resp.ID(), id, method)
	}
	if isErr {
		return id, rpcErr.Err()
	}
	return id, resp.DecodeResult(result, opts)
}

// Notify sends a notification: the server dispatches method but produces no
// response. The error reports an empty method, params that cannot be
// marshaled, or a transport failure.
func (c *Client) Notify(ctx context.Context, method string, params any) error {
	opts, _ := c.config()
	if method == "" {
		return errEmptyMethod
	}
	p, err := NewParams(params, opts)
	if err != nil {
		return err
	}
	_, err = c.sender.Send(ctx, NewNotification(method, p))
	return err
}

// Send round-trips req via the underlying Sender. Server-reported errors appear
// in Response.Error, not in the error return; for a notification the Sender's
// response is returned as-is (typically zero).
func (c *Client) Send(ctx context.Context, req Request) (Response, error) {
	return c.sender.Send(ctx, req)
}
