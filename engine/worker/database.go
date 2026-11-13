//go:build js && wasm

package main

import (
	"fmt"
	"syscall/js"

	"github.com/mateusmoutinho/test-go-nasm-cloudflare/deps"
)

// newDatabaseHub implements deps.DatabaseHub on top of Cloudflare Durable
// Objects: each database id is one object with its own SQLite database
// (see engine/js/database.mjs).
func newDatabaseHub(bridge js.Value) deps.DatabaseHub {
	return deps.DatabaseHub{
		Connect: func(id string, creationQuery string) (*deps.Database, error) {
			db := database{bridge: bridge, id: id}
			if _, err := db.call("create", creationQuery); err != nil {
				return nil, err
			}
			return &deps.Database{All: db.all, First: db.first, Run: db.run}, nil
		},
	}
}

type database struct {
	bridge js.Value
	id     string
}

// call runs a method of the Durable Object of this database.
func (d database) call(method string, args ...any) (js.Value, error) {
	return call(d.bridge, "database", append([]any{d.id, method}, args...)...)
}

func (d database) all(query string, args ...any) ([]deps.Row, error) {
	params, err := queryParams(args)
	if err != nil {
		return nil, err
	}
	result, err := d.call("all", query, params) // { columns: [name], rows: [[value]] }
	if err != nil {
		return nil, err
	}

	columns, jsRows := result.Get("columns"), result.Get("rows")
	rows := make([]deps.Row, jsRows.Length())
	for i := range rows {
		rows[i] = deps.Row{}
		for c := 0; c < columns.Length(); c++ {
			rows[i][columns.Index(c).String()] = rowValue(jsRows.Index(i).Index(c))
		}
	}
	return rows, nil
}

func (d database) first(query string, args ...any) (deps.Row, error) {
	rows, err := d.all(query, args...)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	return rows[0], nil
}

func (d database) run(query string, args ...any) (int, error) {
	params, err := queryParams(args)
	if err != nil {
		return 0, err
	}
	changed, err := d.call("run", query, params)
	if err != nil {
		return 0, err
	}
	return changed.Int(), nil
}

// maxExactInteger is the largest integer a JavaScript number holds exactly
// (2^53 - 1). Every number crosses to the database as a JavaScript number.
const maxExactInteger = 1<<53 - 1

// exactInteger tells whether an integer argument reaches the database with
// all its digits. Only the 64-bit types can be too large.
func exactInteger(arg any) bool {
	switch value := arg.(type) {
	case int:
		return -maxExactInteger <= value && value <= maxExactInteger
	case int64:
		return -maxExactInteger <= value && value <= maxExactInteger
	case uint:
		return value <= maxExactInteger
	case uint64:
		return value <= maxExactInteger
	default:
		return true
	}
}

// queryParams converts the arguments of a query into a JavaScript array.
func queryParams(args []any) (js.Value, error) {
	params := js.Global().Get("Array").New(len(args))
	for i, arg := range args {
		if !exactInteger(arg) { // better an error than a different number in the database
			return js.Undefined(), fmt.Errorf("query argument %d (%v) is too large to be stored exactly: "+
				"pass integers beyond ±%d as a string", i+1, arg, maxExactInteger)
		}
		switch value := arg.(type) {
		case nil:
			params.SetIndex(i, js.Null())
		case bool: // SQLite has no booleans: true is 1, false is 0
			number := 0
			if value {
				number = 1
			}
			params.SetIndex(i, number)
		case []byte: // BLOB
			params.SetIndex(i, fromBytes(value).Get("buffer"))
		case string, float32, float64,
			int, int8, int16, int32, int64,
			uint, uint8, uint16, uint32, uint64:
			params.SetIndex(i, value)
		default:
			return js.Undefined(), fmt.Errorf("query argument %d has an unsupported type: %T", i+1, arg)
		}
	}
	return params, nil
}

// rowValue converts a value read from the database into Go:
// nil, float64, string or []byte.
func rowValue(value js.Value) any {
	switch value.Type() {
	case js.TypeNumber:
		return value.Float()
	case js.TypeString:
		return value.String()
	case js.TypeObject: // BLOB: an ArrayBuffer
		return toBytes(js.Global().Get("Uint8Array").New(value))
	default:
		return nil
	}
}
