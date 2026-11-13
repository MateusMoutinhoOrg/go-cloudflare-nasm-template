// Package deps declares everything a sandbox can use: the current request and
// its response (Server), SQLite databases (DatabaseHub) and requests to other
// servers (HTTPClient).
//
// Every operation is a function field, so implementations can be assigned
// (or swapped) at runtime. A nil field means "not provided"; calling it panics.
package deps

// Deps is what SandboxMain receives, once for every request.
type Deps struct {
	Server      Server
	DatabaseHub DatabaseHub
	HTTPClient  HTTPClient
}
