//go:build js && wasm

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"syscall/js"

	"github.com/MateusMoutinhoOrg/go-cloudflare-nasm-template/deps"
)

// response implements deps.Response.
//
// The status and the headers are kept here until the first write to the body.
// At that point they are sent to JavaScript ("start") and, from then on, each
// write goes straight to the client ("write"), so large bodies are streamed.
type response struct {
	bridge  js.Value // start, write and end of engine/js/worker.mjs
	status  int
	headers []header // in the order they were set

	started bool // the status and the headers were already sent
	hasBody bool // the response was started with a body
	ended   bool // the body was closed
}

type header struct{ name, value string }

func newResponse(bridge js.Value) *response {
	return &response{bridge: bridge, status: 200}
}

func (r *response) api() deps.Response {
	return deps.Response{
		SetStatus: r.setStatus,
		SetHeader: r.setHeader,
		AddHeader: r.addHeader,
		DelHeader: r.delHeader,

		Write: r.Write,

		Text: func(status int, body string) error {
			return r.Bytes(status, "text/plain; charset=utf-8", []byte(body))
		},
		JSON: func(status int, v any) error {
			body, err := json.Marshal(v)
			if err != nil {
				return err
			}
			return r.Bytes(status, "application/json", body)
		},
		Bytes: r.Bytes,
		Stream: func(status int, contentType string, body io.Reader) error {
			r.setStatus(status)
			r.setHeader("Content-Type", contentType)
			_, err := io.Copy(r, body)
			return err
		},
		Redirect: func(status int, location string) error {
			r.setStatus(status)
			r.setHeader("Location", location)
			return r.sendWithoutBody()
		},
		NoContent: func() error {
			r.setStatus(204)
			return r.sendWithoutBody()
		},
	}
}

// ------------------------------------------------------- status and headers

// Once the response has started, changes to the status and to the headers
// are ignored: they are already on their way to the client.

func (r *response) setStatus(code int) {
	if !r.started {
		r.status = code
	}
}

func (r *response) setHeader(name, value string) {
	r.delHeader(name)
	r.addHeader(name, value)
}

func (r *response) addHeader(name, value string) {
	if !r.started {
		r.headers = append(r.headers, header{name, value})
	}
}

func (r *response) delHeader(name string) {
	if r.started {
		return
	}
	kept := r.headers[:0]
	for _, h := range r.headers {
		if !strings.EqualFold(h.name, name) {
			kept = append(kept, h)
		}
	}
	r.headers = kept
}

// --------------------------------------------------------------------- body

// Write sends a piece of the body. The first call sends the status and the
// headers before it. It returns when JavaScript has taken the piece, which
// keeps a slow client from piling up the whole body in memory.
func (r *response) Write(p []byte) (int, error) {
	if !r.started {
		if err := r.start(true); err != nil {
			return 0, err
		}
	}
	if !r.hasBody {
		return 0, fmt.Errorf("a response with status %d cannot have a body", r.status)
	}
	if r.ended {
		return 0, errors.New("the response was already finished")
	}
	if len(p) == 0 {
		return 0, nil
	}
	if _, err := call(r.bridge, "write", fromBytes(p)); err != nil {
		return 0, err
	}
	return len(p), nil
}

// Bytes sets the status and the Content-Type, then writes the body.
func (r *response) Bytes(status int, contentType string, body []byte) error {
	r.setStatus(status)
	r.setHeader("Content-Type", contentType)
	_, err := r.Write(body)
	return err
}

func (r *response) sendWithoutBody() error {
	if r.started {
		return errors.New("the response was already started")
	}
	return r.start(false)
}

// start sends the status and the headers to JavaScript.
// It fails when JavaScript refuses them (e.g. status 999, invalid header name).
func (r *response) start(withBody bool) (err error) {
	defer func() {
		if failure := recover(); failure != nil {
			err = fmt.Errorf("%v", failure)
		}
	}()

	withBody = withBody && allowsBody(r.status)
	headers := make([]any, len(r.headers)) // [[name, value], ...]
	for i, h := range r.headers {
		headers[i] = []any{h.name, h.value}
	}

	r.bridge.Call("start", r.status, headers, withBody)
	r.started, r.hasBody = true, withBody
	return nil
}

// allowsBody tells whether HTTP allows a body in a response with this status.
func allowsBody(status int) bool {
	return status >= 200 && status != 204 && status != 205 && status != 304
}

// finish sends whatever is still missing when the sandbox returns.
func (r *response) finish() {
	if !r.started {
		if err := r.start(false); err != nil {
			r.fail(err)
			return
		}
	}
	if r.hasBody && !r.ended {
		r.ended = true
		r.bridge.Call("end")
	}
}

// fail is the end of a request that went wrong: the reason goes to the log,
// never to the client. If nothing was sent yet, the client gets a 500 error;
// otherwise the body is left open and engine/js/worker.mjs cuts it short when
// this program exits, so the client can tell that the response is incomplete.
func (r *response) fail(reason any) {
	logError(fmt.Sprint("the request failed: ", reason))
	if r.started {
		return
	}

	r.status, r.headers = 500, nil
	if err := r.Bytes(500, "text/plain; charset=utf-8", []byte("Internal Server Error")); err != nil {
		logError(fmt.Sprint("could not send the 500 error: ", err))
		return
	}
	r.finish()
}
