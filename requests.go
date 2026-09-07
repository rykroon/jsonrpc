package jsonrpc

import (
	"encoding/json/jsontext"
	"errors"

	"encoding/json/v2"
)

const Version = "2.0"

// Request is a JSON-RPC 2.0 request, or a notification when len(ID) == 0.
// Params and ID stay raw because the spec leaves their types open; decode them
// at the point of use.
type Request struct {
	JSONRPC string         `json:"jsonrpc"`
	Method  string         `json:"method"`
	Params  jsontext.Value `json:"params,omitzero"`
	ID      jsontext.Value `json:"id,omitzero"`
}

var _ json.UnmarshalerFrom = (*Request)(nil)

func (r *Request) IsNotification() bool {
	return len(r.ID) == 0
}

// UnmarshalJSONFrom decodes one request object, walking it token by token so
// every rejection carries a message this package wrote rather than one from the
// JSON library: an unrecognized envelope member, a non-string method, or
// non-structured params are Invalid Request, while malformed input is a Parse
// error. A Server's RequestDecoder overrides this, and can delegate here.
//
// Duplicate member names are rejected anywhere, including inside params, since
// detection is tokenizer-level. Params content is otherwise not validated, so
// unknown members inside it are the handler's concern. Per the spec params must
// be an object or array whenever present, so null is rejected like any other
// scalar; a request with no parameters omits the member.
//
// Required members are not checked here: Serve rejects a missing method or
// wrong version on every path.
//
// The Request is reset first, since json/v2 does not zero the destination
// before calling this method and a member this message omits must not be
// carried over from a previous one — a stale id would make a notification look
// like a request. Members are then filled in place rather than assigned at the
// end, unlike Response.UnmarshalJSONFrom: a failure partway through keeps
// whatever was read before it, which is how the server recovers an id to
// attribute an error response to.
func (req *Request) UnmarshalJSONFrom(d *jsontext.Decoder) error {
	*req = Request{}

	tok, err := d.ReadToken()
	if err != nil {
		return tokenError(err)
	}
	if tok.Kind() != jsontext.KindBeginObject {
		return NewError(CodeInvalidRequest, "request must be a JSON object")
	}

	for d.PeekKind() != jsontext.KindEndObject {
		tok, err := d.ReadToken()
		if err != nil {
			return tokenError(err)
		}
		// Token.String allocates, so the name stays valid across the reads
		// below; the Token itself does not.
		switch name := tok.String(); name {
		case "jsonrpc":
			tok, err := d.ReadToken()
			if err != nil {
				return tokenError(err)
			}
			if tok.Kind() != jsontext.KindString {
				return NewError(CodeInvalidRequest, "jsonrpc must be a string")
			}
			// Whether the version is "2.0" is Serve's verdict: failing here
			// would discard an id we can still read.
			req.JSONRPC = tok.String()

		case "method":
			tok, err := d.ReadToken()
			if err != nil {
				return tokenError(err)
			}
			if tok.Kind() != jsontext.KindString {
				return NewError(CodeInvalidRequest, "method must be a string")
			}
			req.Method = tok.String()

		case "params":
			val, err := d.ReadValue()
			if err != nil {
				return tokenError(err)
			}
			switch val.Kind() {
			case jsontext.KindBeginObject, jsontext.KindBeginArray:
				// ReadValue's buffer is only valid until the next read.
				req.Params = jsontext.Value(val.Clone())
			default:
				// Including null: the member is present but unstructured.
				// Omit params entirely to send none.
				return NewError(CodeInvalidRequest, "params must be an object or array")
			}

		case "id":
			val, err := d.ReadValue()
			if err != nil {
				return tokenError(err)
			}
			req.ID = jsontext.Value(val.Clone())

		default:
			return NewError(CodeInvalidRequest, "unknown member: "+name)
		}
	}

	// Consume the closing brace, completing the one value this decoder reads.
	if _, err := d.ReadToken(); err != nil {
		return tokenError(err)
	}
	return nil
}

// tokenError maps a tokenizer failure to the spec's codes: a duplicate member
// name is well-formed JSON violating uniqueness (Invalid Request), anything
// else is malformed input (Parse error).
func tokenError(err error) *Error {
	if errors.Is(err, jsontext.ErrDuplicateName) {
		return NewError(CodeInvalidRequest, err.Error())
	}
	return NewError(CodeParseError, err.Error())
}

// NewRequest assembles a Request. Params and id are raw JSON; build them with
// NewParams and NewID. For a notification, use NewNotification.
func NewRequest(method string, params, id jsontext.Value) *Request {
	return &Request{JSONRPC: Version, Method: method, Params: params, ID: id}
}

// NewNotification assembles a Request without an id, so the server sends no
// response.
func NewNotification(method string, params jsontext.Value) *Request {
	return &Request{JSONRPC: Version, Method: method, Params: params}
}

// NewID returns the JSON encoding of v for use as Request.ID. The constraint
// matches the spec-allowed id shapes; marshal other types directly.
func NewID[T ~string | ~int | ~int64 | ~uint64](v T) jsontext.Value {
	b, _ := json.Marshal(v)
	return b
}

// NewParams marshals v for use as Request.Params. A nil v returns nil; a
// jsontext.Value passes through unchanged.
func NewParams(v any) (jsontext.Value, error) {
	if v == nil {
		return nil, nil
	}
	if raw, ok := v.(jsontext.Value); ok {
		return raw, nil
	}
	return json.Marshal(v)
}
