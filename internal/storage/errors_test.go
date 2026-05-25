package storage

import (
	"errors"
	"testing"
)

func TestErrSchemaVersionMismatch_Error(t *testing.T) {
	tests := []struct {
		name    string
		dbVer   int
		fileVer int
		wantMsg string
	}{
		{
			name:    "version one ahead",
			dbVer:   1,
			fileVer: 2,
			wantMsg: "schema version mismatch: database has version 1, schema.json has version 2",
		},
		{
			name:    "version behind",
			dbVer:   5,
			fileVer: 2,
			wantMsg: "schema version mismatch: database has version 5, schema.json has version 2",
		},
		{
			name:    "zero versions",
			dbVer:   0,
			fileVer: 1,
			wantMsg: "schema version mismatch: database has version 0, schema.json has version 1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &SchemaVersionMismatchError{
				DBVersion:   tt.dbVer,
				FileVersion: tt.fileVer,
			}
			if got := err.Error(); got != tt.wantMsg {
				t.Errorf("Error() = %q, want %q", got, tt.wantMsg)
			}
		})
	}
}

func TestErrSchemaVersionMismatch_Is(t *testing.T) {
	err := &SchemaVersionMismatchError{DBVersion: 1, FileVersion: 2}
	if !errors.Is(err, ErrSchemaVersionMismatch) {
		t.Error("expected errors.Is(err, ErrSchemaVersionMismatch) to be true")
	}

	// A different error type should not match.
	otherErr := errors.New("some other error")
	if errors.Is(otherErr, ErrSchemaVersionMismatch) {
		t.Error("expected errors.Is(otherErr, ErrSchemaVersionMismatch) to be false")
	}
}

func TestErrSchemaVersionMismatch_As(t *testing.T) {
	err := &SchemaVersionMismatchError{DBVersion: 1, FileVersion: 2}

	var target *SchemaVersionMismatchError
	if !errors.As(err, &target) {
		t.Fatal("expected errors.As to succeed")
	}
	if target.DBVersion != 1 {
		t.Errorf("DBVersion = %d, want 1", target.DBVersion)
	}
	if target.FileVersion != 2 {
		t.Errorf("FileVersion = %d, want 2", target.FileVersion)
	}
}
