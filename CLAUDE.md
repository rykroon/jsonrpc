# CLAUDE.md

# References
- JSON-RPC Specification: https://www.jsonrpc.org/specification
- Google Golang style guide: https://google.github.io/styleguide/go/

# JSON-RPC types

The JSON-RPC types just represent data and therfore should be implemented as value types (non-pointer types). They should be immutable in the correct context. For example a client should not be able to alter the response it gets back from a server. And the server should not be able to alter the request it receives from the client.

The types should be written in a way so that an instance of a type can never express an invalid state as described in the JSONRPC specification. For example Params can only ever be a JSON object or array (or be empty). An ID can only ever be a string, number, or null (or empty).

type Request struct {}

type Response struct {}

type ID struct {}
For IDs, on the client side, one should only ever be able to create a StringID() or an IntID(). But on the server side it is more laxed as the server is just meant to echo the id back to the client. So explicitely null ids and numbers containing fractional parts are allowed on the server side. However anything other than that should be considered an invalid state.

type Params struct {}

type Error struct {}
An Error should implement the error interface.


# Architecture

The jsonrpc package should be Transport agnostic. Subpackages can be created for transport-specific impementations, such as jsonrpchttp

## Client Side

The sender interface is designed to send a request over a specific transport. (HTTP, Websocks, etc).

The client encapsulates everything needed for sending a jsonrpc request, including the sender.

## Server Side
A server can register methods. Method handlers can be in the form of a "Typed" Handler or a RawHandler.

A RawHandler passes the Params and expects a jsontext.Value for the result.
A typed Handler passes P any (The Params type) and expects T any (The Result type.)
