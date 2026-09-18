// Package store wraps modernc.org/sqlite (pure-Go, no cgo) with two store
// types — RelayStore (server side) and PCStore (client side) — plus a tiny
// migration runner driven by embedded SQL files. Each test opens a fresh
// in-memory database, so the tests are isolated, deterministic, and run
// without touching the real user data directory.
//
// Design notes:
//   - All public store constructors accept a *sql.DB, never open their own
//     connection, so tests inject a controlled handle and production code
//     can pool.
//   - Migrations are a lexicographically ordered set of *.sql files under
//     internal/store/migrations/{relay,pc}, embedded into the binary.
//   - The package never imports relayproto (the wire types). Conversion
//     between wire and stored representations happens in the caller, which
//     keeps the storage layer focused on rows and SQL.
package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed migrations/relay/*.sql migrations/pc/*.sql
var migrationFS embed.FS

// connectionParams are applied by the driver to every connection it opens, so
// pooled connections created later inherit them too. Running the same settings
// once through db.Exec would configure only whichever pooled connection served
// that call; busy_timeout and foreign_keys are per-connection in SQLite, so
// every later connection would silently run with the defaults (no timeout, no
// cascade enforcement).
//
//   - _pragma=busy_timeout(5000): wait for a contended lock instead of failing
//     immediately with SQLITE_BUSY.
//   - _pragma=foreign_keys(1): enforce ON DELETE CASCADE, which is OFF by
//     default and is what DeleteProject/DeleteClient rely on.
//   - _pragma=journal_mode(WAL): readers alongside the single writer. This one
//     is persisted in the database header, so re-applying it per connection is
//     a no-op after the first.
//   - _txlock=immediate: begin transactions with BEGIN IMMEDIATE so they take
//     the write lock up front. A deferred transaction that reads and then
//     writes cannot be rescued by busy_timeout: when its read snapshot goes
//     stale, SQLite returns SQLITE_BUSY / SQLITE_BUSY_SNAPSHOT without
//     consulting the busy handler.
var connectionParams = []string{
	"_txlock=immediate",
	"_pragma=busy_timeout(5000)",
	"_pragma=foreign_keys(1)",
	"_pragma=journal_mode(WAL)",
}

// withConnectionParams appends the driver query parameters to a path or URI.
// modernc.org/sqlite keeps the query out of the filename unless the DSN starts
// with "file:", so a plain filesystem path (Windows paths included) is passed
// through to the OS untouched.
func withConnectionParams(path string) string {
	sep := "?"
	if strings.ContainsRune(path, '?') {
		sep = "&"
	}
	return path + sep + strings.Join(connectionParams, "&")
}

// Open opens (or creates) a SQLite database at path. The caller is
// responsible for Close. For tests, prefer OpenInMemory.
//
// modernc.org/sqlite registers the "sqlite" driver via its blank import in
// driver_register.go; this file keeps an indirect dependency rather than a
// blank import here so the seam is visible to readers.
func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", withConnectionParams(path))
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	// sql.Open is lazy. Connect once so an unusable path fails here rather than
	// at first use, and so the per-connection parameters are actually applied.
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	return db, nil
}

// OpenInMemory opens a private in-memory database whose lifetime is tied to
// the supplied name. Tests pass t.Name() (or similar) so each test gets an
// isolated database and multiple connections within the same test see the
// same data. The caller is responsible for Close.
//
// We deliberately don't auto-generate the name with a package counter because
// that would add package-level mutable runtime state. Passing the name in keeps
// the helper pure.
func OpenInMemory(name string) (*sql.DB, error) {
	return Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", name))
}

// MigrateRelay runs all relay migrations in order against db.
func MigrateRelay(ctx context.Context, db *sql.DB) error {
	return migrate(ctx, db, "migrations/relay")
}

// MigratePC runs all PC migrations in order against db.
func MigratePC(ctx context.Context, db *sql.DB) error {
	return migrate(ctx, db, "migrations/pc")
}

// migrate lists all *.sql files under subdir in the embedded FS and runs
// unapplied files in lexicographic order. Migrations predating the ledger are
// deliberately idempotent, so an existing database can run them once more and
// record them before moving on to one-shot table rebuilds.
func migrate(ctx context.Context, db *sql.DB, subdir string) error {
	entries, err := fs.ReadDir(migrationFS, subdir)
	if err != nil {
		return fmt.Errorf("store: read migrations %s: %w", subdir, err)
	}
	files := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	if _, err := db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		scope TEXT NOT NULL,
		name TEXT NOT NULL,
		applied_at INTEGER NOT NULL DEFAULT (unixepoch()),
		PRIMARY KEY (scope, name)
	)`); err != nil {
		return fmt.Errorf("store: create migration ledger: %w", err)
	}

	for _, name := range files {
		var applied int
		if err := db.QueryRowContext(ctx,
			"SELECT 1 FROM schema_migrations WHERE scope = ? AND name = ?",
			subdir, name,
		).Scan(&applied); err == nil {
			continue
		} else if err != sql.ErrNoRows {
			return fmt.Errorf("store: check migration %s/%s: %w", subdir, name, err)
		}
		path := subdir + "/" + name
		b, err := migrationFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("store: read %s: %w", path, err)
		}
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("store: begin migration %s: %w", path, err)
		}
		if err := execScript(ctx, tx, path, string(b)); err != nil {
			_ = tx.Rollback()
			return err
		}
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO schema_migrations (scope, name) VALUES (?, ?)", subdir, name,
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("store: record migration %s: %w", path, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("store: commit migration %s: %w", path, err)
		}
	}
	return nil
}

type migrationExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// execScript runs the raw SQL in a file. Comment lines (starting with --)
// are stripped first, then the remaining content is split on ';' so
// multi-statement scripts work with database/sql, whose Exec only accepts a
// single statement. Stripping comments before splitting is essential: SQL
// comments may themselves contain semicolons, which would naively split a
// comment's tail into a phantom statement (SQLite then reports a syntax
// error on the leftover text). Empty statements are skipped.
func execScript(ctx context.Context, db migrationExecer, name, script string) error {
	// Remove full-line and trailing -- comments. We only strip from the first
	// -- on each line to keep the parser simple; no inline /* */ blocks in our
	// migrations. This is good enough and easy to audit.
	lines := strings.Split(script, "\n")
	for i, line := range lines {
		if idx := strings.Index(line, "--"); idx >= 0 {
			lines[i] = line[:idx]
		}
	}
	cleaned := strings.Join(lines, "\n")

	for _, stmt := range strings.Split(cleaned, ";") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			// Older migrations were designed to be replayed and may add columns.
			// Existing installations execute each of those once while seeding the
			// ledger, so tolerate a column that is already present.
			if strings.Contains(err.Error(), "duplicate column name") {
				continue
			}
			return fmt.Errorf("store: migrate %s: %w", name, err)
		}
	}
	return nil
}
