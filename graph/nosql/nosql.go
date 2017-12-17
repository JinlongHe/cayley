package nosql

import (
	"context"
	"errors"
	"github.com/pborman/uuid"
	"time"
)

var (
	ErrNotFound = errors.New("not found")
)

// Key is a set of values that describe primary key of document.
type Key []string

// Value converts a Key to a value that can be stored in the database.
func (k Key) Value() Value {
	return Strings(k)
}

// GenKey generates a unique key (with one field).
func GenKey() Key {
	return Key{uuid.NewUUID().String()}
}

// KeyFrom extracts a set of fields as a Key from Document.
func KeyFrom(fields []string, doc Document) Key {
	key := make(Key, 0, len(fields))
	for _, f := range fields {
		if s, ok := doc[f].(String); ok {
			key = append(key, string(s))
		}
	}
	return key
}

// Database is a minimal interface for NoSQL database implementations.
type Database interface {
	// Insert creates a document with a given key in a given collection.
	// Key can be nil meaning that implementation should generate a unique key for the item.
	// It returns the key that was generated, or the same key that was passed to it.
	Insert(ctx context.Context, col string, key Key, d Document) (Key, error)
	// FindByKey finds a document by it's Key. It returns ErrNotFound if document not exists.
	FindByKey(ctx context.Context, col string, key Key) (Document, error)
	// Query starts construction of a new query for a specified collection.
	Query(col string) Query
	// Update starts construction of document update request for a specified document and collection.
	Update(col string, key Key) Update
	// Delete starts construction of document delete request.
	Delete(col string) Delete
	// EnsureIndex creates or updates indexes on the collection to match it's arguments.
	// It should create collection if it not exists. Primary index is guaranteed to be of StringExact type.
	EnsureIndex(ctx context.Context, col string, primary Index, secondary []Index) error
	// Close closes the database connection.
	Close() error
}

// FilterOp is a comparison operation type used for value filters.
type FilterOp int

const (
	Equal = FilterOp(iota)
	NotEqual
	GT
	GTE
	LT
	LTE
)

// FieldFilter represents a single field comparison operation.
type FieldFilter struct {
	Path   []string // path is a path to specific field in the document
	Filter FilterOp // comparison operation
	Value  Value    // value that will be compared with field of the document
}

// Query is a query builder object.
type Query interface {
	// WithFields adds specified filters to the query.
	WithFields(filters ...FieldFilter) Query
	// Limit limits a maximal number of results returned.
	Limit(n int) Query

	// Count executes query and returns a number of items that matches it.
	Count(ctx context.Context) (int64, error)
	// One executes query and returns first document from it.
	One(ctx context.Context) (Document, error)
	// Iterate starts an iteration over query results.
	Iterate() DocIterator
}

// Update is an update request builder.
type Update interface {
	// Inc increments document field with a given amount. Will also increment upserted document.
	Inc(field string, dn int) Update
	// Upsert sets a document that will be inserted in case original object does not exists already.
	// It should omit fields used by Inc - they will be added automatically.
	Upsert(d Document) Update
	// Do executes update request.
	Do(ctx context.Context) error
}

// Update is a batch delete request builder.
type Delete interface {
	// WithFields adds specified filters to select document for deletion.
	WithFields(filters ...FieldFilter) Delete
	// Keys limits a set of documents to delete to ones with keys specified.
	// Delete still uses provided filters, thus it will not delete objects with these keys if they do not pass filters.
	Keys(keys ...Key) Delete
	// Do executes batch delete.
	Do(ctx context.Context) error
}

// DocIterator is an iterator over a list of documents.
type DocIterator interface {
	// Next advances an iterator to the next document.
	Next(ctx context.Context) bool
	// Err returns a last encountered error.
	Err() error
	// Close frees all resources associated with iterator.
	Close() error
	// Key returns a key of current document.
	Key() Key
	// Doc returns current document.
	Doc() Document
}

// BatchInsert returns a streaming writer for database or emulates it if database has no support for batch inserts.
func BatchInsert(db Database, col string) DocWriter {
	if bi, ok := db.(BatchInserter); ok {
		return bi.BatchInsert(col)
	}
	return &seqInsert{db: db, col: col}
}

type seqInsert struct {
	db   Database
	col  string
	keys []Key
	err  error
}

func (w *seqInsert) WriteDoc(ctx context.Context, key Key, d Document) error {
	key, err := w.db.Insert(ctx, w.col, key, d)
	if err != nil {
		w.err = err
		return err
	}
	w.keys = append(w.keys, key)
	return nil
}

func (w *seqInsert) Flush(ctx context.Context) error {
	return w.err
}

func (w *seqInsert) Keys() []Key {
	return w.keys
}

func (w *seqInsert) Close() error {
	return w.err
}

// DocWriter is an interface for writing documents in streaming manner.
type DocWriter interface {
	// WriteDoc prepares document to be written. Write becomes valid only after Flush.
	WriteDoc(ctx context.Context, key Key, d Document) error
	// Flush waits for all writes to complete.
	Flush(ctx context.Context) error
	// Keys returns a list of already inserted documents.
	// Might be less then a number of written documents until Flush is called.
	Keys() []Key
	// Close closes writer and discards any unflushed documents.
	Close() error
}

// BatchInserter is an optional interface for databases that can insert documents in batches.
type BatchInserter interface {
	BatchInsert(col string) DocWriter
}

// IndexType is a type of index for collection.
type IndexType int

const (
	IndexAny    = IndexType(iota)
	StringExact // exact match for string values (usually a hash index)

	//StringFulltext
	//IntIndex
	//FloatIndex
	//TimeIndex
)

// Index is an index for a collection of documents.
type Index struct {
	Fields []string // an ordered set of fields used in index
	Type   IndexType
}

// Value is a interface that limits a set of types that nosql database can handle.
type Value interface {
	isValue()
}

// Document is a type of item stored in nosql database.
type Document map[string]Value

func (Document) isValue() {}

// String is an UTF8 string value.
type String string

func (String) isValue() {}

// Int is an int value.
//
// Some databases might not distinguish Int value from Float.
// In this case implementation will take care of converting it to a correct type.
type Int int64

func (Int) isValue() {}

// Float is an floating point value.
//
// Some databases might not distinguish Int value from Float.
// In this case the package will take care of converting it to a correct type.
type Float float64

func (Float) isValue() {}

// Bool is a boolean value.
type Bool bool

func (Bool) isValue() {}

// Time is a timestamp value.
//
// Some databases has no type to represent time values.
// In this case string/json representation can be used and package will take care of converting it.
type Time time.Time

func (Time) isValue() {}

// Bytes is a raw binary data.
//
// Some databases has no type to represent binary data.
// In this case base64 representation can be used and package will take care of converting it.
type Bytes []byte

func (Bytes) isValue() {}

// Strings is an array of strings. Used mostly to store Keys.
type Strings []string

func (Strings) isValue() {}