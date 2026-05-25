package storage

import "fmt"

// SchemaVersionMismatchError is returned when an existing database's
// recorded schema version does not match the schema.json version.
//
// The caller can use this error to decide whether to trigger a migration
// path (Epic 2) or refuse to start.
type SchemaVersionMismatchError struct {
	DBVersion   int
	FileVersion int
}

// Error implements the error interface.
func (e *SchemaVersionMismatchError) Error() string {
	return fmt.Sprintf("schema version mismatch: database has version %d, schema.json has version %d",
		e.DBVersion, e.FileVersion)
}

// Is enables errors.Is(err, ErrSchemaVersionMismatch) for any
// *SchemaVersionMismatchError regardless of the version numbers.
func (e *SchemaVersionMismatchError) Is(target error) bool {
	return target == ErrSchemaVersionMismatch
}

// ErrSchemaVersionMismatch is the sentinel error for schema version
// mismatch detection.
var ErrSchemaVersionMismatch = &SchemaVersionMismatchError{}
