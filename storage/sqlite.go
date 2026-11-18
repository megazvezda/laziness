package storage

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

func Open(ctx context.Context, path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	// create the file with restricted permissions
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("create database file: %w", err)
	}
	// restrict permissions if the file already existed.
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return nil, fmt.Errorf("set database permissions: %w", err)
	}
	if err := file.Close(); err != nil {
		return nil, fmt.Errorf("close database file: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	db.SetMaxOpenConns(1)

	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("initialize database: %w", err)
	}

	return db, nil
}
