//go:build js && wasm

package main

import (
	"strings"
	"syscall/js"
	"testing"
)

func TestCall(t *testing.T) {
	object := js.Global().Call("eval", `({
		resolves: async (a, b) => a + b,
		rejects: async () => { throw new Error("no such table"); },
		throws: () => { throw new TypeError("invalid URL"); },
	})`)

	if value, err := call(object, "resolves", 2, 3); err != nil || value.Int() != 5 {
		t.Errorf("a resolved Promise: %v, %v", value, err)
	}
	if _, err := call(object, "rejects"); err == nil || !strings.Contains(err.Error(), "no such table") {
		t.Errorf("a rejected Promise: err = %v", err)
	}
	if _, err := call(object, "throws"); err == nil || !strings.Contains(err.Error(), "invalid URL") {
		t.Errorf("a thrown exception: err = %v", err)
	}
}

func TestBytes(t *testing.T) {
	for _, bytes := range [][]byte{{}, {0}, {1, 2, 3, 255}} {
		if got := toBytes(fromBytes(bytes)); string(got) != string(bytes) {
			t.Errorf("%v became %v after a round trip through JavaScript", bytes, got)
		}
	}
}
