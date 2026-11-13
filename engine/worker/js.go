//go:build js && wasm

package main

import (
	"errors"
	"fmt"
	"syscall/js"
)

// call runs a JavaScript method that returns a Promise and waits for its
// value. A thrown exception and a rejected Promise both become an error.
func call(object js.Value, method string, args ...any) (value js.Value, err error) {
	defer func() {
		if failure := recover(); failure != nil {
			value, err = js.Undefined(), fmt.Errorf("%v", failure)
		}
	}()
	return await(object.Call(method, args...))
}

// await blocks until the JavaScript Promise settles.
// While waiting, the Go runtime yields control back to JavaScript.
func await(promise js.Value) (js.Value, error) {
	resolved := make(chan js.Value, 1)
	rejected := make(chan js.Value, 1)

	onResolve := js.FuncOf(func(_ js.Value, args []js.Value) any {
		resolved <- first(args)
		return nil
	})
	onReject := js.FuncOf(func(_ js.Value, args []js.Value) any {
		rejected <- first(args)
		return nil
	})
	defer onResolve.Release()
	defer onReject.Release()

	promise.Call("then", onResolve, onReject)

	select {
	case value := <-resolved:
		return value, nil
	case reason := <-rejected:
		return js.Undefined(), errors.New(text(reason))
	}
}

func first(args []js.Value) js.Value {
	if len(args) == 0 {
		return js.Undefined()
	}
	return args[0]
}

// text is what JavaScript's String(value) returns, e.g. "Error: no such table".
func text(value js.Value) string {
	return js.Global().Get("String").Invoke(value).String()
}

// missing tells whether a JavaScript value is null or undefined.
func missing(value js.Value) bool {
	return value.IsNull() || value.IsUndefined()
}

// toBytes copies a JavaScript Uint8Array into Go.
func toBytes(array js.Value) []byte {
	bytes := make([]byte, array.Get("byteLength").Int())
	js.CopyBytesToGo(bytes, array)
	return bytes
}

// fromBytes copies Go bytes into a new JavaScript Uint8Array.
func fromBytes(bytes []byte) js.Value {
	array := js.Global().Get("Uint8Array").New(len(bytes))
	js.CopyBytesToJS(array, bytes)
	return array
}

// logError writes to the Worker log (the terminal of `wrangler dev`).
func logError(message string) {
	js.Global().Get("console").Call("error", message)
}
