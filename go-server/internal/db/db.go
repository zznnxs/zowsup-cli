// Package db opens the modernc.org/sqlite database used by the Go rewrite
// of zowsup-cli and applies versioned migrations from migrations/*.sql.
package db

import (
	"database/sql"
	"embed"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed all:migrations/*.sql
var migrationsFS embed.FS

// Open opens (creating if missing) the sqlite database at path, applies any
// outstanding migrations from the embedded migrations directory, and returns
// the *sql.DB ready for use. WAL mode and foreign keys are enabled.
func Open(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", path)
	d, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if err := d.Ping(); err != nil {
		_ = d.Close()
		return nil, fmt.Errorf("ping %s: %w", path, err)
	}
	if err := migrate(d); err != nil {
		_ = d.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return d, nil
}

// migrate applies every file under migrations/ whose name starts with a
// numeric version not yet recorded in schema_migrations.
func migrate(d *sql.DB) error {
	if _, err := d.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at INTEGER NOT NULL)`); err != nil {
		return err
	}
	applied, err := loadApplied(d)
	if err != nil {
		return err
	}
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	type migration struct {
		version int
		name    string
	}
	var pending []migration
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		v, err := versionFromName(e.Name())
		if err != nil {
			return err
		}
		if _, ok := applied[v]; ok {
			continue
		}
		pending = append(pending, migration{version: v, name: e.Name()})
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].version < pending[j].version })
	for _, m := range pending {
		body, err := migrationsFS.ReadFile("migrations/" + m.name)
		if err != nil {
			return err
		}
		tx, err := d.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply %s: %w", m.name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations(version, applied_at) VALUES (?, ?)`,
			m.version, time.Now().Unix()); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func loadApplied(d *sql.DB) (map[int]struct{}, error) {
	out := map[int]struct{}{}
	rows, err := d.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out[v] = struct{}{}
	}
	return out, rows.Err()
}

// versionFromName extracts the leading integer from "001_init.sql".
func versionFromName(name string) (int, error) {
	i := 0
	for i < len(name) && name[i] >= '0' && name[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0, fmt.Errorf("migration filename %q must start with a numeric version", name)
	}
	return strconv.Atoi(name[:i])
}
