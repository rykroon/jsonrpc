package jsonrpchttp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/rykroon/jsonrpc"
)

// Sender is a jsonrpc.Sender that round-trips Requests over HTTP. Each
// Send POSTs the marshaled Request to URL and decodes one Response from
// the reply body. Notifications discard the response body and return
// (nil, nil).
//
// Custom headers and authentication are not handled here; wrap Client to
// add a Transport that injects them, or compose a jsonrpc.SenderFunc around
// this Sender.
type Sender struct {
	// URL is the JSON-RPC endpoint. Required.
	URL string

	// Client is the HTTP client used for round-trips. If nil,
	// http.DefaultClient is used.
	Client *http.Client
}

// Send implements jsonrpc.Sender.
func (s *Sender) Send(ctx context.Context, req *jsonrpc.Request) (*jsonrpc.Response, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("jsonrpchttp: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.URL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	client := s.Client
	if client == nil {
		client = http.DefaultClient
	}

	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// A non-2xx reply (proxy 502, server 500, ...) carries no JSON-RPC
	// response; surface it as a transport error rather than a decode failure.
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		io.Copy(io.Discard, resp.Body)
		return nil, fmt.Errorf("jsonrpchttp: unexpected HTTP status %s", resp.Status)
	}

	if req.IsNotification() {
		io.Copy(io.Discard, resp.Body)
		return nil, nil
	}

	if resp.StatusCode == http.StatusNoContent {
		return nil, nil
	}

	// Read the body fully so the underlying connection can be reused.
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("jsonrpchttp: read response: %w", err)
	}
	var jr jsonrpc.Response
	if err := json.Unmarshal(data, &jr); err != nil {
		return nil, fmt.Errorf("jsonrpchttp: decode response: %w", err)
	}
	return &jr, nil
}
