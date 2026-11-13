//go:build js && wasm

package main

import (
	"strings"
	"syscall/js"
	"testing"
)

// fakeBridge stands in for the bridge of engine/js/worker.mjs: it records
// what Go sends. Like the real one, start throws when the status or a header
// is not valid.
func fakeBridge() js.Value {
	return js.Global().Call("eval", `({
		status: 0, headers: null, hasBody: false, body: "", ended: false,
		start(status, headers, hasBody) {
			this.headers = [...new Response(null, { status, headers }).headers].join("|");
			this.status = status;
			this.hasBody = hasBody;
		},
		write(chunk) {
			this.body += new TextDecoder().decode(chunk);
			return Promise.resolve();
		},
		end() { this.ended = true; },
	})`)
}

// sent checks what reached the bridge when the request is over.
func sent(t *testing.T, bridge js.Value, status int, headers, body string) {
	t.Helper()
	if got := bridge.Get("status").Int(); got != status {
		t.Errorf("status = %d, want %d", got, status)
	}
	if got := bridge.Get("headers").String(); got != headers {
		t.Errorf("headers = %q, want %q", got, headers)
	}
	if got := bridge.Get("body").String(); got != body {
		t.Errorf("body = %q, want %q", got, body)
	}
	if hasBody, ended := bridge.Get("hasBody").Bool(), bridge.Get("ended").Bool(); hasBody != ended {
		t.Errorf("the response has a body: %v, but the body was closed: %v", hasBody, ended)
	}
}

func TestResponseText(t *testing.T) {
	bridge := fakeBridge()
	response := newResponse(bridge)
	if err := response.api().Text(201, "created"); err != nil {
		t.Fatal(err)
	}
	response.finish()
	sent(t, bridge, 201, "content-type,text/plain; charset=utf-8", "created")
}

func TestResponseJSON(t *testing.T) {
	bridge := fakeBridge()
	response := newResponse(bridge)
	if err := response.api().JSON(200, map[string]any{"ok": true}); err != nil {
		t.Fatal(err)
	}
	response.finish()
	sent(t, bridge, 200, "content-type,application/json", `{"ok":true}`)
}

func TestResponseHeaders(t *testing.T) {
	bridge := fakeBridge()
	response := newResponse(bridge)
	api := response.api()

	api.SetHeader("X-One", "1")
	api.SetHeader("x-one", "2") // replaces, whatever the case of the name
	api.AddHeader("X-Many", "a")
	api.AddHeader("X-Many", "b")
	api.SetHeader("X-Gone", "x")
	api.DelHeader("x-gone")
	api.SetStatus(202)
	api.Write([]byte("first "))

	// The response has started: none of this reaches the client.
	api.SetStatus(500)
	api.SetHeader("X-Late", "1")
	api.DelHeader("X-One")

	api.Write([]byte("second"))
	response.finish()
	sent(t, bridge, 202, "x-many,a, b|x-one,2", "first second")
}

func TestResponseStream(t *testing.T) {
	bridge := fakeBridge()
	response := newResponse(bridge)
	if err := response.api().Stream(200, "text/csv", strings.NewReader("a,b\n1,2\n")); err != nil {
		t.Fatal(err)
	}
	response.finish()
	sent(t, bridge, 200, "content-type,text/csv", "a,b\n1,2\n")
}

func TestResponseWithoutBody(t *testing.T) {
	t.Run("nothing written", func(t *testing.T) {
		bridge := fakeBridge()
		newResponse(bridge).finish()
		sent(t, bridge, 200, "", "")
	})

	t.Run("NoContent", func(t *testing.T) {
		bridge := fakeBridge()
		response := newResponse(bridge)
		api := response.api()
		if err := api.NoContent(); err != nil {
			t.Fatal(err)
		}
		if _, err := api.Write([]byte("x")); err == nil {
			t.Error("a body was accepted after NoContent")
		}
		response.finish()
		sent(t, bridge, 204, "", "")
	})

	t.Run("Redirect", func(t *testing.T) {
		bridge := fakeBridge()
		response := newResponse(bridge)
		if err := response.api().Redirect(302, "/login"); err != nil {
			t.Fatal(err)
		}
		response.finish()
		sent(t, bridge, 302, "location,/login", "")
	})
}

func TestResponseInvalidStatus(t *testing.T) {
	bridge := fakeBridge()
	response := newResponse(bridge)
	if err := response.api().Text(999, "x"); err == nil {
		t.Error("status 999 was accepted")
	}
	response.finish() // nothing valid was sent: the client gets a 500
	sent(t, bridge, 500, "content-type,text/plain; charset=utf-8", "Internal Server Error")
}

func TestResponseFail(t *testing.T) {
	t.Run("before the response started", func(t *testing.T) {
		bridge := fakeBridge()
		response := newResponse(bridge)
		response.api().SetHeader("X-Secret", "1") // set for the response that failed
		response.fail("boom")
		sent(t, bridge, 500, "content-type,text/plain; charset=utf-8", "Internal Server Error")
	})

	t.Run("after the response started", func(t *testing.T) {
		bridge := fakeBridge()
		response := newResponse(bridge)
		response.api().Write([]byte("half"))
		response.fail("boom")
		// The body is left open: worker.mjs cuts it short when Go exits.
		if bridge.Get("ended").Bool() {
			t.Error("a response that failed halfway was finished as if it were complete")
		}
	})
}
