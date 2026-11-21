package main

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"time"
)

// Миграции схемы: нумерованные .sql из backend/migrations, применяются
// по именам через таблицу schema_version. go:embed — чтобы бинарь был
// самодостаточен (ADR-0001).

//go:embed migrations/*.sql
var migrationFS embed.FS

func migrateDB(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (
		name TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("schema_version: %w", err)
	}

	applied := map[string]bool{}
	rows, err := db.Query(`SELECT name FROM schema_version`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return err
		}
		applied[name] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	files, err := fs.ReadDir(migrationFS, "migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(files))
	for _, f := range files {
		names = append(names, f.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		if applied[name] {
			continue
		}
		content, err := migrationFS.ReadFile("migrations/" + name)
		if err != nil {
			return err
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(content)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("миграция %s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_version (name, applied_at) VALUES (?, ?)`,
			name, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
