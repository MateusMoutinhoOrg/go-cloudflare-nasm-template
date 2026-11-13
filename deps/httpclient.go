package deps

import "io"

// HTTPClient makes HTTP and HTTPS requests to other servers.
//
// A request returns an error only when there is no response (invalid URL,
// server unreachable, ...). A status such as 404 or 500 is a response, not an
// error. Redirects are followed.
type HTTPClient struct {
	// Do sends a request. headers and body may be nil.
	// It returns when the status and the headers of the response arrive;
	// the body is read through the HTTPResponse.
	Do func(method, url string, headers map[string]string, body []byte) (*HTTPResponse, error)

	// Helpers for the most common requests.
	Get      func(url string) (*HTTPResponse, error)
	PostJSON func(url string, v any) (*HTTPResponse, error) // sends v as JSON
}

// HTTPResponse gives read access to the response of another server.
type HTTPResponse struct {
	Status func() int // 200, 404, ...

	// Response headers. Names are case-insensitive.
	Header       func(name string) string
	HeaderValues func(name string) []string
	LookupHeader func(name string) (string, bool)
	HeaderNames  func() []string

	// Body returns the response body as a stream.
	// The body can be read only once: Body, ReadBody and BindJSON
	// all consume the same stream.
	Body func() io.Reader

	// ReadBody reads the whole body, up to max bytes.
	// It returns an error if the body is larger than max.
	ReadBody func(max int64) ([]byte, error)

	// BindJSON decodes the body (up to max bytes) into v.
	BindJSON func(v any, max int64) error
}
