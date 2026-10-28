// Package store owns Boombrick's SQLite database and its schema migrations.
package store

import (
	"database/sql"
	"fmt"

	_ "github.com/mattn/go-sqlite3"
)

// Store is the open database.
type Store struct {
	DB *sql.DB
}

// Open opens or creates the database at path and brings its schema up to date.
// Each connection keeps 512 KiB of pages rather than SQLite's 2 MiB: the
// cache is C memory, outside Go's memory limit.
func Open(path string) (*Store, error) {
	dsn := path + "?_journal_mode=WAL&_busy_timeout=5000&_synchronous=NORMAL&_cache_size=-512"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate %s: %w", path, err)
	}
	return &Store{DB: db}, nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.DB.Close()
}
