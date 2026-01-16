package main

import (
	"log"
	"net/http"

	"github.com/rykroon/jsonrpc"
)

type Arith struct{}

type AddParams struct {
	X int
	Y int
}

func (a Arith) Add(params AddParams, reply *int) error {
	*reply = params.X + params.Y
	return nil
}

func main() {
	jsonRpcServer := jsonrpc.NewServer()
	jsonRpcServer.RegisterName("add", &Arith{})
	http.Handle("/jsonrpc", jsonrpc.NewHttpHandler(jsonRpcServer))
	log.Println("Starting server...")
	http.ListenAndServe(":8080", nil)
}
