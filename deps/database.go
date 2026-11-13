package deps

// Row is a single result row, keyed by column name.
// Values are nil, float64 (every SQL number), string or []byte (BLOB).
type Row = map[string]any

// Database is a SQLite database.
//
// Use ?1, ?2, ... in the query and pass the values in args. A value may be
// nil, a bool (stored as 1 or 0), a number, a string or a []byte (BLOB).
// An integer beyond ±2^53 is refused, because it would lose digits on the
// way: pass it as a string.
//
// A query made of several statements ("...; ...") is all or nothing: if one
// of them fails, the ones before it are undone.
type Database struct {
	// All runs the query and returns every row.
	All func(query string, args ...any) ([]Row, error)

	// First runs the query and returns the first row, or nil if there are no rows.
	First func(query string, args ...any) (Row, error)

	// Run executes a write (INSERT, UPDATE, DELETE) and returns how many rows changed.
	Run func(query string, args ...any) (int, error)
}

// DatabaseHub gives access to sandbox databases by id.
type DatabaseHub struct {
	// Connect returns the database with the given id.
	// If it does not exist yet, it is created and creationQuery runs once.
	// A database that already exists is not touched: changing creationQuery
	// later does not change its tables.
	Connect func(id string, creationQuery string) (*Database, error)
}
