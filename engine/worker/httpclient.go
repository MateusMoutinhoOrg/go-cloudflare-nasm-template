//go:build js && wasm

package main

import (
	"encoding/json"
	"io"
	"syscall/js"

	"github.com/mateusmoutinho/test-go-nasm-cloudflare/deps"
)

// newHTTPClient implements deps.HTTPClient on top of JavaScript's fetch
// (https://developer.mozilla.org/docs/Web/API/fetch).
func newHTTPClient() deps.HTTPClient {
	return deps.HTTPClient{
		Do: fetch,
		Get: func(url string) (*deps.HTTPResponse, error) {
			return fetch("GET", url, nil, nil)
		},
		PostJSON: func(url string, v any) (*deps.HTTPResponse, error) {
			body, err := json.Marshal(v)
			if err != nil {
				return nil, err
			}
			return fetch("POST", url, map[string]string{"Content-Type": "application/json"}, body)
		},
	}
}

// fetch sends the request and waits for the status and the headers of the
// response. It fails when there is no response (invalid URL, no connection).
func fetch(method, url string, headers map[string]string, body []byte) (*deps.HTTPResponse, error) {
	jsHeaders := map[string]any{}
	for name, value := range headers {
		jsHeaders[name] = value
	}
	options := map[string]any{"method": method, "headers": jsHeaders}
	if len(body) > 0 { // JavaScript refuses a GET with a body, even an empty one
		options["body"] = fromBytes(body)
	}

	response, err := call(js.Global(), "fetch", url, options) // a JavaScript Response
	if err != nil {
		return nil, err
	}

	responseHeaders := readHeaders(response.Get("headers"))
	responseBody := newBody(response.Get("body"))

	return &deps.HTTPResponse{
		Status: func() int { return response.Get("status").Int() },

		Header:       responseHeaders.get,
		HeaderValues: responseHeaders.all,
		LookupHeader: responseHeaders.lookup,
		HeaderNames:  func() []string { return responseHeaders.names },

		Body:     func() io.Reader { return responseBody },
		ReadBody: responseBody.readAll,
		BindJSON: responseBody.bindJSON,
	}, nil
}
