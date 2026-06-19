package jsonrpchttp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rykroon/jsonrpc"
	"github.com/rykroon/jsonrpc/jsonrpchttp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newServer(t *testing.T) *jsonrpc.Server {
	t.Helper()
	s := jsonrpc.NewServer()
	jsonrpc.Register(s, "addOne", func(_ context.Context, n int) (int, error) {
		return n + 1, nil
	})
	return s
}

func newTestHTTP(t *testing.T) (*httptest.Server, *jsonrpc.Client) {
	t.Helper()
	ts := httptest.NewServer(&jsonrpchttp.Handler{Server: newServer(t)})
	t.Cleanup(ts.Close)
	client := jsonrpc.NewClient(&jsonrpchttp.Sender{URL: ts.URL})
	return ts, client
}

func TestHandlerRoundTrip(t *testing.T) {
	_, client := newTestHTTP(t)

	params, err := jsonrpc.NewParams(7)
	require.NoError(t, err)

	resp, err := client.Send(context.Background(), jsonrpc.NewRequest("addOne", params, jsonrpc.NewID(1)))
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Nil(t, resp.Error)

	var result int
	require.NoError(t, resp.Decode(&result))
	assert.Equal(t, 8, result)
}

func TestHandlerRejectsNonPost(t *testing.T) {
	ts, _ := newTestHTTP(t)

	resp, err := http.Get(ts.URL)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
	assert.Equal(t, "POST", resp.Header.Get("Allow"))
}

func TestHandlerNotificationReturns204(t *testing.T) {
	ts, _ := newTestHTTP(t)

	resp, err := http.Post(ts.URL, "application/json",
		strings.NewReader(`{"jsonrpc":"2.0","method":"addOne","params":1}`))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNoContent, resp.StatusCode)
}

func TestSenderNotificationReturnsNil(t *testing.T) {
	_, client := newTestHTTP(t)

	resp, err := client.Send(context.Background(), jsonrpc.NewNotification("addOne", nil))
	require.NoError(t, err)
	assert.Nil(t, resp)
}

func TestHandlerReturnsMethodNotFound(t *testing.T) {
	_, client := newTestHTTP(t)

	resp, err := client.Send(context.Background(), jsonrpc.NewRequest("missing", nil, jsonrpc.NewID("x")))
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Error)
	assert.Equal(t, jsonrpc.CodeMethodNotFound, resp.Error.Code)
}

func TestHandlerParseError(t *testing.T) {
	ts, _ := newTestHTTP(t)

	resp, err := http.Post(ts.URL, "application/json", strings.NewReader(`{"jsonrpc":"2.0","method":`))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestHandlerAcceptsContentTypeWithParams(t *testing.T) {
	ts, _ := newTestHTTP(t)

	resp, err := http.Post(ts.URL, "application/json; charset=utf-8",
		strings.NewReader(`{"jsonrpc":"2.0","method":"addOne","params":1,"id":1}`))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestHandlerRejectsNonJSONContentType(t *testing.T) {
	ts, _ := newTestHTTP(t)

	for _, ct := range []string{"text/plain", ""} {
		resp, err := http.Post(ts.URL, ct,
			strings.NewReader(`{"jsonrpc":"2.0","method":"addOne","params":1,"id":1}`))
		require.NoError(t, err)
		resp.Body.Close()
		assert.Equal(t, http.StatusUnsupportedMediaType, resp.StatusCode, "content-type %q", ct)
	}
}

func TestHandlerEnforcesMaxBodyBytes(t *testing.T) {
	h := &jsonrpchttp.Handler{Server: newServer(t), MaxBodyBytes: 16}
	ts := httptest.NewServer(h)
	defer ts.Close()

	big := `{"jsonrpc":"2.0","method":"addOne","params":1,"id":1}`
	resp, err := http.Post(ts.URL, "application/json", strings.NewReader(big))
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}
