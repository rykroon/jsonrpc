package jsonrpc

import (
	"encoding/json/jsontext"
	"errors"
	"fmt"
)

// ErrInvalidResponse reports a reply that is valid JSON but not a valid
// response to the call: not a response object (no id, both result and error,
// a wrong version, ...), no response at all, or one whose id is not the
// request's. Malformed JSON is reported as a *jsontext.SyntacticError instead.
// Match it with errors.Is.
var ErrInvalidResponse = errors.New("jsonrpc: invalid response")

// invalidResponse wraps ErrInvalidResponse with a detail.
func invalidResponse(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInvalidResponse}, args...)...)
}

// isSyntaxError reports malformed JSON. A duplicate member name is a
// SyntacticError to jsontext but valid JSON grammar, so it is not one.
func isSyntaxError(err error) bool {
	_, ok := errors.AsType[*jsontext.SyntacticError](err)
	return ok && !errors.Is(err, jsontext.ErrDuplicateName)
}
