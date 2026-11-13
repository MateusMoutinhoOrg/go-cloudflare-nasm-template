package deps

import (
	"context"
	"io"
)

// Request gives a handler read access to the current HTTP request.
//
// Getters that return a string return "" when the value is missing.
// Use the Lookup* variants when "missing" and "empty" must be told apart.
type Request struct {
	// Context is canceled when the client disconnects or the request ends.
	Context func() context.Context

	Method     func() string // "GET", "POST", ...
	Route      func() string // request path, e.g. "/users/42"
	RemoteAddr func() string // IP address of the client, without the port

	// Match tells whether the route has the form of pattern. A part of the
	// pattern written as ":name" accepts any text, so Match("/users/:id") is
	// true for "/users/42" (and false for "/users" and "/users/42/edit").
	Match func(pattern string) bool

	// Route parameters: the ":name" parts of the last pattern that matched,
	// e.g. after Match("/users/:id"), Param("id") == "42".
	Param       func(name string) string
	LookupParam func(name string) (string, bool)

	// Query string values, e.g. "?tag=a&tag=b".
	Query       func(name string) string   // first value
	QueryValues func(name string) []string // all values
	LookupQuery func(name string) (string, bool)

	// Request headers. Names are case-insensitive.
	Header       func(name string) string
	HeaderValues func(name string) []string
	LookupHeader func(name string) (string, bool)
	HeaderNames  func() []string

	// Cookie returns the value of a request cookie.
	Cookie func(name string) (string, bool)

	// Body returns the request body as a stream.
	// The body can be read only once: Body, ReadBody and BindJSON
	// all consume the same stream.
	Body func() io.Reader

	// ReadBody reads the whole body, up to max bytes.
	// It returns an error if the body is larger than max.
	ReadBody func(max int64) ([]byte, error)

	// BindJSON decodes the body (up to max bytes) into v.
	BindJSON func(v any, max int64) error
}

// Response lets a handler write the HTTP response.
//
// Status and headers must be set before the first write to the body;
// changes made after that are ignored.
type Response struct {
	SetStatus func(code int)

	SetHeader func(name, value string) // replaces existing values
	AddHeader func(name, value string) // appends a value
	DelHeader func(name string)

	// Write writes raw bytes to the response body.
	Write func(p []byte) (int, error)

	// Helpers that set the status and Content-Type, then write the body.
	Text      func(status int, body string) error
	JSON      func(status int, v any) error
	Bytes     func(status int, contentType string, body []byte) error
	Stream    func(status int, contentType string, r io.Reader) error
	Redirect  func(status int, location string) error
	NoContent func() error // 204
}

// Server gives a handler access to the current request and its response.
// Fields of Request and Response are promoted, so s.Param("id") and
// s.JSON(200, v) work directly on a *Server.
type Server struct {
	Request
	Response
}
