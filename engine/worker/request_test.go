//go:build js && wasm

package main

import (
	"context"
	"reflect"
	"strings"
	"syscall/js"
	"testing"
)

func TestMatchRoute(t *testing.T) {
	tests := []struct {
		pattern, path string
		params        map[string]string // nil: must not match
	}{
		{"/", "/", map[string]string{}},
		{"/api/links", "/api/links", map[string]string{}},
		{"/api/links", "/api/links/", nil},
		{"/api/links", "/api", nil},
		{"/users/:id", "/users/42", map[string]string{"id": "42"}},
		{"/users/:id", "/users", nil},
		{"/users/:id", "/users/", nil},
		{"/users/:id", "/users/42/edit", nil},
		{"/users/:id", "/people/42", nil},
		{"/:a/x/:b", "/1/x/2", map[string]string{"a": "1", "b": "2"}},
		{"/files/:name", "/files/a%2Fb%20c", map[string]string{"name": "a/b c"}},
		{"/a b", "/a%20b", map[string]string{}},
		{"/users/:id", "/users/%zz", nil},
	}
	for _, test := range tests {
		params, matches := matchRoute(test.pattern, test.path)
		if matches != (test.params != nil) || (matches && !reflect.DeepEqual(params, test.params)) {
			t.Errorf("matchRoute(%q, %q) = %v, %v; want %v", test.pattern, test.path, params, matches, test.params)
		}
	}
}

func TestFindCookie(t *testing.T) {
	const header = `session=abc; theme="dark"; empty=`
	tests := []struct {
		name, value string
		found       bool
	}{
		{"session", "abc", true},
		{"theme", "dark", true},
		{"empty", "", true},
		{"missing", "", false},
		{"sess", "", false},
	}
	for _, test := range tests {
		value, found := findCookie(header, test.name)
		if value != test.value || found != test.found {
			t.Errorf("findCookie(%q) = %q, %v; want %q, %v", test.name, value, found, test.value, test.found)
		}
	}
}

// jsRequest creates a JavaScript Request, like the one the Worker receives.
func jsRequest(address, method, body string, headers map[string]any) js.Value {
	options := map[string]any{"method": method, "headers": headers}
	if body != "" {
		options["body"] = body
	}
	return js.Global().Get("Request").New(address, options)
}

func TestRequest(t *testing.T) {
	request := newRequest(context.Background(), jsRequest(
		"https://example.com/users/42?tag=a&tag=b&empty=", "POST", `{"name":"Ana"}`,
		map[string]any{"X-Token": "secret", "Cookie": "session=abc"},
	))

	if got := request.Method(); got != "POST" {
		t.Errorf("Method() = %q", got)
	}
	if got := request.Route(); got != "/users/42" {
		t.Errorf("Route() = %q", got)
	}

	if request.Match("/users") {
		t.Error(`Match("/users") is true for /users/42`)
	}
	if _, found := request.LookupParam("id"); found {
		t.Error("there is a route parameter before any pattern matched")
	}
	if !request.Match("/users/:id") || request.Param("id") != "42" {
		t.Errorf(`after Match("/users/:id"), Param("id") = %q`, request.Param("id"))
	}
	if request.Match("/posts/:post") || request.Param("id") != "42" {
		t.Error("a pattern that does not match changed the route parameters")
	}

	if got := request.Query("tag"); got != "a" {
		t.Errorf(`Query("tag") = %q`, got)
	}
	if got := request.QueryValues("tag"); !reflect.DeepEqual(got, []string{"a", "b"}) {
		t.Errorf(`QueryValues("tag") = %v`, got)
	}
	if value, found := request.LookupQuery("empty"); value != "" || !found {
		t.Errorf(`LookupQuery("empty") = %q, %v`, value, found)
	}
	if _, found := request.LookupQuery("missing"); found {
		t.Error(`LookupQuery("missing") found a value`)
	}

	if got := request.Header("x-token"); got != "secret" { // names are case-insensitive
		t.Errorf(`Header("x-token") = %q`, got)
	}
	if _, found := request.LookupHeader("X-Missing"); found {
		t.Error(`LookupHeader("X-Missing") found a value`)
	}
	if value, found := request.Cookie("session"); value != "abc" || !found {
		t.Errorf(`Cookie("session") = %q, %v`, value, found)
	}

	var input struct{ Name string }
	if err := request.BindJSON(&input, 100); err != nil || input.Name != "Ana" {
		t.Errorf("BindJSON: %+v, %v", input, err)
	}
	if rest, err := request.ReadBody(100); err != nil || len(rest) != 0 { // the body is read only once
		t.Errorf("second read of the body = %q, %v", rest, err)
	}
}

func TestRequestWithoutBody(t *testing.T) {
	request := newRequest(context.Background(), jsRequest("https://example.com/", "GET", "", nil))
	if data, err := request.ReadBody(10); err != nil || len(data) != 0 {
		t.Errorf("ReadBody = %q, %v", data, err)
	}
}

func TestReadBodyLimit(t *testing.T) {
	read := func(max int64) ([]byte, error) {
		request := jsRequest("https://example.com/", "POST", "0123456789", nil)
		return newRequest(context.Background(), request).ReadBody(max)
	}

	if data, err := read(10); err != nil || string(data) != "0123456789" {
		t.Errorf("ReadBody(10) of 10 bytes = %q, %v", data, err)
	}
	if _, err := read(9); err == nil || !strings.Contains(err.Error(), "larger than 9 bytes") {
		t.Errorf("ReadBody(9) of 10 bytes: err = %v", err)
	}
}

func TestReadHeaders(t *testing.T) {
	jsHeaders := js.Global().Get("Headers").New()
	jsHeaders.Call("append", "X-List", "a")
	jsHeaders.Call("append", "x-list", "b")
	jsHeaders.Call("append", "Set-Cookie", "a=1")
	jsHeaders.Call("append", "Set-Cookie", "b=2")
	headers := readHeaders(jsHeaders)

	if got := headers.get("X-LIST"); got != "a, b" {
		t.Errorf(`get("X-LIST") = %q`, got)
	}
	if got := headers.all("set-cookie"); !reflect.DeepEqual(got, []string{"a=1", "b=2"}) {
		t.Errorf(`all("set-cookie") = %v`, got)
	}
	if got := headers.names; !reflect.DeepEqual(got, []string{"set-cookie", "x-list"}) {
		t.Errorf("names = %v", got)
	}
}
