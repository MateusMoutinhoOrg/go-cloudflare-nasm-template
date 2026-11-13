//go:build js && wasm

package main

import (
	"math"
	"reflect"
	"strings"
	"syscall/js"
	"testing"
)

func TestQueryParams(t *testing.T) {
	params, err := queryParams([]any{nil, true, false, 42, int64(-7), uint8(255), 1.5, "olá", []byte{1, 2, 255}})
	if err != nil {
		t.Fatal(err)
	}

	// JSON shows the type JavaScript received. The BLOB (an ArrayBuffer,
	// which JSON prints as {}) is checked apart.
	blob := params.Call("pop")
	if got := js.Global().Get("JSON").Call("stringify", params).String(); got != `[null,1,0,42,-7,255,1.5,"olá"]` {
		t.Errorf("params = %s", got)
	}
	if !blob.InstanceOf(js.Global().Get("ArrayBuffer")) || blob.Get("byteLength").Int() != 3 {
		t.Errorf("a []byte must become an ArrayBuffer of the same size")
	}
}

func TestQueryParamsRefusesWhatWouldBeStoredWrong(t *testing.T) {
	const exact = 1<<53 - 1
	accepted := []any{exact, int64(-exact), uint64(exact), float64(1 << 60)}
	if _, err := queryParams(accepted); err != nil {
		t.Errorf("queryParams(%v): %v", accepted, err)
	}

	refused := []any{exact + 1, int64(-exact - 1), uint64(exact + 1), int64(math.MaxInt64), uint(math.MaxUint64)}
	for _, value := range refused {
		_, err := queryParams([]any{"fine", value})
		if err == nil || !strings.Contains(err.Error(), "query argument 2") {
			t.Errorf("queryParams(%T %v): err = %v", value, value, err)
		}
	}

	if _, err := queryParams([]any{struct{}{}}); err == nil {
		t.Error("a struct was accepted as a query argument")
	}
}

func TestRowValue(t *testing.T) {
	buffer := fromBytes([]byte{0, 1, 255}).Get("buffer")
	tests := []struct {
		value js.Value
		want  any
	}{
		{js.Null(), nil},
		{js.ValueOf(3), 3.0},
		{js.ValueOf(1.5), 1.5},
		{js.ValueOf("text"), "text"},
		{js.ValueOf(""), ""},
		{buffer, []byte{0, 1, 255}},
	}
	for _, test := range tests {
		if got := rowValue(test.value); !reflect.DeepEqual(got, test.want) {
			t.Errorf("rowValue(%v) = %#v, want %#v", test.value, got, test.want)
		}
	}
}
