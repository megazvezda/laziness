package storage

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/megazvezda/laziness/models"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

func SaveSession(ctx context.Context, db *sql.DB, record models.DBRecord) error {
	if record.TelegramUserID <= 0 || record.MoodleUserID <= 0 {
		return fmt.Errorf("save session: Telegram and Moodle user IDs must be positive")
	}
	if strings.TrimSpace(record.PublicToken) == "" {
		return fmt.Errorf("save session: public token is required")
	}

	_, err := db.ExecContext(ctx, `
		INSERT INTO moodle_sessions (
			telegram_user_id, moodle_user_id, moodle_fullname, public_token, private_token
		) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(telegram_user_id) DO UPDATE SET
			moodle_user_id = excluded.moodle_user_id,
			moodle_fullname = excluded.moodle_fullname,
			public_token = excluded.public_token,
			private_token = excluded.private_token
	`, record.TelegramUserID, record.MoodleUserID, record.FullName, record.PublicToken, record.PrivateToken)
	if err != nil {
		return fmt.Errorf("save Moodle session: %w", err)
	}
	return nil
}

// RetrieveSession - If no session exists, the returned error wraps sql.ErrNoRows.
func RetrieveSession(ctx context.Context, db *sql.DB, telegramUserID int64) (models.DBRecord, error) {
	if telegramUserID <= 0 {
		return models.DBRecord{}, fmt.Errorf("retrieve session: Telegram user ID must be positive")
	}

	var record models.DBRecord
	err := db.QueryRowContext(ctx, `
		SELECT telegram_user_id, moodle_user_id, moodle_fullname, public_token, private_token
		FROM moodle_sessions
		WHERE telegram_user_id = ?
	`, telegramUserID).Scan(
		&record.TelegramUserID, &record.MoodleUserID, &record.FullName,
		&record.PublicToken, &record.PrivateToken,
	)
	if err != nil {
		return models.DBRecord{}, fmt.Errorf("retrieve Moodle session: %w", err)
	}
	return record, nil
}

// DeleteSession removes the stored Moodle tokens for a Telegram user.
// The returned bool reports whether a session existed.
func DeleteSession(ctx context.Context, db *sql.DB, telegramUserID int64) (bool, error) {
	if telegramUserID <= 0 {
		return false, fmt.Errorf("delete session: Telegram user ID must be positive")
	}

	result, err := db.ExecContext(ctx, `
		DELETE FROM moodle_sessions
		WHERE telegram_user_id = ?
	`, telegramUserID)
	if err != nil {
		return false, fmt.Errorf("delete Moodle session: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("delete Moodle session: %w", err)
	}
	return deleted > 0, nil
}

func ListSessions(ctx context.Context, db *sql.DB) ([]models.DBRecord, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT telegram_user_id, moodle_user_id, moodle_fullname, public_token, private_token
		FROM moodle_sessions
		ORDER BY telegram_user_id
	`)
	if err != nil {
		return nil, fmt.Errorf("list Moodle sessions: %w", err)
	}
	defer rows.Close()

	var records []models.DBRecord
	for rows.Next() {
		var record models.DBRecord
		if err := rows.Scan(
			&record.TelegramUserID, &record.MoodleUserID, &record.FullName,
			&record.PublicToken, &record.PrivateToken,
		); err != nil {
			return nil, fmt.Errorf("read Moodle session: %w", err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list Moodle sessions: %w", err)
	}
	return records, nil
}

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
		_ = file.Close()
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
		_ = db.Close()
		return nil, fmt.Errorf("initialize database: %w", err)
	}

	return db, nil
}
