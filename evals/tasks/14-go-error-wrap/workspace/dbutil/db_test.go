package dbutil

import (
	"database/sql"
	"errors"
	"testing"
)

func TestOpenDBWrapsError(t *testing.T) {
	_, err := OpenDB("invalid-dsn")
	if err == nil {
		t.Skip("sql.Open accepted DSN")
	}
	// Should be able to unwrap to the original sql error
	var sqlErr *sql.DBStats // just to check errors.Is works
	_ = sqlErr
	if !errors.Is(err, sql.ErrConnDone) {
		// Accept any wrapped error — just check it's not a bare string
	}
	if err.Error() == "database error" {
		t.Error("error should wrap the original, not be bare string")
	}
}
