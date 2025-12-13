package main

import (
	"log"
	"net/http"

	"github.com/rykroon/jsonrpc"
)

func main() {
	jsonRpcServer := jsonrpc.NewServer()
	http.Handle("/jsonrpc", jsonrpc.NewHttpHandler(jsonRpcServer))
	log.Println("Starting server...")
	http.ListenAndServe(":8080", nil)
}
