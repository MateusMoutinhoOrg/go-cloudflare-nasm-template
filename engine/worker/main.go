//go:build js && wasm

// The Go program that runs inside the Cloudflare Worker, compiled to
// WebAssembly. It connects the two sides of the project:
//
//   - /sandbox: what the server does, written against the /deps API;
//   - engine/js/worker.mjs: the Cloudflare Worker that receives the requests.
//
// One instance of this program handles one request:
//  1. engine/js/worker.mjs receives the request and starts the program;
//  2. main builds the deps (request, response, databases, HTTP client) on top
//     of JavaScript;
//  3. sandbox.SandboxMain handles the request using only those deps.
package main

import (
	"os"
	"syscall/js"

	"github.com/mateusmoutinho/test-go-nasm-cloudflare/deps"
	"github.com/mateusmoutinho/test-go-nasm-cloudflare/sandbox"
)

func main() {
	bridge := currentBridge()
	request := bridge.Get("request")
	response := newResponse(bridge)

	ctx, endContext := requestContext(request)
	defer endContext()

	serve(response, deps.Deps{
		Server: deps.Server{
			Request:  newRequest(ctx, request),
			Response: response.api(),
		},
		DatabaseHub: newDatabaseHub(bridge),
		HTTPClient:  newHTTPClient(),
	})
}

// serve runs the sandbox and finishes the response.
// A panic in the sandbox becomes a 500 error.
func serve(response *response, sandboxDeps deps.Deps) {
	defer func() {
		if failure := recover(); failure != nil {
			response.fail(failure)
		}
	}()

	sandbox.SandboxMain(sandboxDeps)
	response.finish()
}

// currentBridge fetches what engine/js/worker.mjs left for this instance of
// the program (identified by the environment variable): the JavaScript
// request, plus the functions that send the response and reach the databases.
func currentBridge() js.Value {
	id := os.Getenv("REQUEST_ID")
	bridge := js.Global().Get("__goRequests").Call("get", id)
	if bridge.IsUndefined() {
		panic("request " + id + " not found (the program must be started by engine/js/worker.mjs)")
	}
	return bridge
}
