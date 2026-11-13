//go:build js && wasm

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/url"
	"strings"
	"syscall/js"

	"github.com/MateusMoutinhoOrg/go-cloudflare-nasm-template/deps"
)

// newRequest implements deps.Request on top of the JavaScript Request
// (https://developer.mozilla.org/docs/Web/API/Request).
func newRequest(ctx context.Context, request js.Value) deps.Request {
	address, err := url.Parse(request.Get("url").String())
	if err != nil {
		address = &url.URL{Path: "/"}
	}
	query := address.Query()
	headers := readHeaders(request.Get("headers"))
	body := newBody(request.Get("body"))
	params := map[string]string{} // of the last pattern that matched

	return deps.Request{
		Context: func() context.Context { return ctx },

		Method: func() string { return request.Get("method").String() },
		Route:  func() string { return address.Path },

		// Cloudflare gives the address of the client, but not its port.
		RemoteAddr: func() string { return headers.get("CF-Connecting-IP") },

		Match: func(pattern string) bool {
			found, matches := matchRoute(pattern, address.EscapedPath())
			if matches {
				params = found
			}
			return matches
		},
		Param: func(name string) string { return params[name] },
		LookupParam: func(name string) (string, bool) {
			value, found := params[name]
			return value, found
		},

		Query:       func(name string) string { return query.Get(name) },
		QueryValues: func(name string) []string { return query[name] },
		LookupQuery: func(name string) (string, bool) {
			values, found := query[name]
			return query.Get(name), found && len(values) > 0
		},

		Header:       headers.get,
		HeaderValues: headers.all,
		LookupHeader: headers.lookup,
		HeaderNames:  func() []string { return headers.names },

		Cookie: func(name string) (string, bool) {
			return findCookie(headers.get("Cookie"), name)
		},

		Body:     func() io.Reader { return body },
		ReadBody: body.readAll,
		BindJSON: body.bindJSON,
	}
}

// requestContext returns a context that is canceled when the client
// disconnects. The returned function must be called when the request ends.
func requestContext(request js.Value) (context.Context, func()) {
	ctx, cancel := context.WithCancel(context.Background())

	signal := request.Get("signal") // AbortSignal
	if missing(signal) {
		return ctx, cancel
	}

	onAbort := js.FuncOf(func(js.Value, []js.Value) any {
		cancel()
		return nil
	})
	signal.Call("addEventListener", "abort", onAbort)
	if signal.Get("aborted").Bool() {
		cancel()
	}

	return ctx, func() {
		signal.Call("removeEventListener", "abort", onAbort)
		onAbort.Release()
		cancel()
	}
}

// ------------------------------------------------------------------- routes

// matchRoute compares the path of a request with a pattern such as
// "/users/:id". The two must have the same parts, between the slashes. A part
// of the pattern that starts with ":" accepts any text except an empty one,
// and that text becomes a parameter: {"id": "42"}.
//
// The path arrives as it was sent ("/files/a%2Fb"), so that an encoded slash
// stays inside its part: the parameter of "/files/:name" is "a/b".
func matchRoute(pattern, escapedPath string) (map[string]string, bool) {
	wanted := strings.Split(pattern, "/")
	received := strings.Split(escapedPath, "/")
	if len(wanted) != len(received) {
		return nil, false
	}

	params := map[string]string{}
	for i, part := range wanted {
		value, err := url.PathUnescape(received[i])
		if err != nil {
			return nil, false
		}
		if name, isParam := strings.CutPrefix(part, ":"); isParam && name != "" {
			if value == "" {
				return nil, false
			}
			params[name] = value
		} else if part != value {
			return nil, false
		}
	}
	return params, true
}

// ------------------------------------------------------------------ headers

// receivedHeaders holds the headers of a message that came from JavaScript:
// the request, or the response to a request made with deps.HTTPClient.
// Names are stored in lower case, which is how JavaScript lists them.
type receivedHeaders struct {
	names  []string
	values map[string][]string
}

// readHeaders copies the JavaScript Headers into Go.
//
// JavaScript joins repeated headers into a single value ("a, b"), so in
// practice every name has exactly one value. The exception is Set-Cookie,
// which is listed once per cookie.
func readHeaders(headers js.Value) receivedHeaders {
	result := receivedHeaders{values: map[string][]string{}}

	pairs := js.Global().Get("Array").Call("from", headers) // [[name, value], ...]
	for i := 0; i < pairs.Length(); i++ {
		name := strings.ToLower(pairs.Index(i).Index(0).String())
		value := pairs.Index(i).Index(1).String()

		if _, known := result.values[name]; !known {
			result.names = append(result.names, name)
		}
		result.values[name] = append(result.values[name], value)
	}
	return result
}

func (h receivedHeaders) all(name string) []string {
	return h.values[strings.ToLower(name)]
}

func (h receivedHeaders) get(name string) string {
	return strings.Join(h.all(name), ", ")
}

// lookup also tells whether the header is present.
func (h receivedHeaders) lookup(name string) (string, bool) {
	return h.get(name), len(h.all(name)) > 0
}

// findCookie looks for a cookie in the value of the Cookie header,
// e.g. "session=abc; theme=dark".
func findCookie(header, name string) (string, bool) {
	for _, pair := range strings.Split(header, ";") {
		key, value, found := strings.Cut(strings.TrimSpace(pair), "=")
		if !found || key != name {
			continue
		}
		if len(value) >= 2 && strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
			value = value[1 : len(value)-1]
		}
		return value, true
	}
	return "", false
}

// --------------------------------------------------------------------- body

// body reads the body of a message that came from JavaScript (the request, or
// the response to a request made with deps.HTTPClient), a ReadableStream, as
// an io.Reader. The stream can be read only once.
type body struct {
	stream  js.Value // null when the message has no body
	reader  js.Value // created on the first Read
	pending []byte   // already received from JavaScript, not yet read
	done    bool
}

func newBody(stream js.Value) *body {
	return &body{stream: stream, done: missing(stream)}
}

func (b *body) Read(p []byte) (int, error) {
	for len(b.pending) == 0 {
		if b.done {
			return 0, io.EOF
		}
		if b.reader.IsUndefined() {
			b.reader = b.stream.Call("getReader")
		}

		chunk, err := call(b.reader, "read") // { done, value: Uint8Array }
		if err != nil {
			return 0, err
		}
		if chunk.Get("done").Bool() {
			b.done = true
		} else {
			b.pending = toBytes(chunk.Get("value"))
		}
	}

	n := copy(p, b.pending)
	b.pending = b.pending[n:]
	return n, nil
}

// readAll reads the whole body, up to max bytes.
// It returns an error if the body is larger than max.
func (b *body) readAll(max int64) ([]byte, error) {
	limit := max
	if limit < math.MaxInt64 {
		limit++ // one extra byte tells "exactly max" from "larger than max"
	}
	data, err := io.ReadAll(io.LimitReader(b, limit))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("the body is larger than %d bytes", max)
	}
	return data, nil
}

// bindJSON decodes the body (up to max bytes) into v.
func (b *body) bindJSON(v any, max int64) error {
	data, err := b.readAll(max)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}
