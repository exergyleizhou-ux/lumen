package dbutil

import "database/sql"

func OpenDB(dsn string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		// BUG: error is swallowed — should be wrapped
		return nil, err
	}
	return db, nil
}
