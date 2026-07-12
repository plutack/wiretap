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

// Open opens (or creates) a SQLite database at path. The caller is
// responsible for Close. For tests, prefer OpenInMemory.
//
// modernc.org/sqlite registers the "sqlite" driver via its blank import in
// driver_register.go; this file keeps an indirect dependency rather than a
// blank import here so the seam is visible to readers.
func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	// Single writer is plenty for our load and avoids SQLITE_BUSY surprises;
	// modernc.org/sqlite supports concurrent reads under WAL.
	for _, pragma := range []string{
		"PRAGMA journal_mode = WAL;",  // concurrent readers alongside the one writer
		"PRAGMA foreign_keys = ON;",   // enforce FK cascades (OFF by default in SQLite)
		"PRAGMA busy_timeout = 5000;", // wait 5s instead of immediate SQLITE_BUSY
	} {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("store: set %q: %w", pragma, err)
		}
	}
	return db, nil
}

// OpenInMemory opens a private in-memory database whose lifetime is tied to
// the supplied name. Tests pass t.Name() (or similar) so each test gets an
// isolated database and multiple connections within the same test see the
// same data. The caller is responsible for Close.
//
// We deliberately don't auto-generate the name with a package counter: that
// would be package-level mutable state, which PLAN.md keeps out of runtime
// code. Passing the name in keeps the helper pure.
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
// them in lexicographic order. Each file is split on ';' into statements;
// empty statements are skipped. A failure halts and returns a wrapped error
// naming the offending file.
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

	for _, name := range files {
		path := subdir + "/" + name
		b, err := migrationFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("store: read %s: %w", path, err)
		}
		if err := execScript(ctx, db, path, string(b)); err != nil {
			return err
		}
	}
	return nil
}

// execScript runs the raw SQL in a file. Comment lines (starting with --)
// are stripped first, then the remaining content is split on ';' so
// multi-statement scripts work with database/sql, whose Exec only accepts a
// single statement. Stripping comments before splitting is essential: SQL
// comments may themselves contain semicolons, which would naively split a
// comment's tail into a phantom statement (SQLite then reports a syntax
// error on the leftover text). Empty statements are skipped.
func execScript(ctx context.Context, db *sql.DB, name, script string) error {
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
			return fmt.Errorf("store: migrate %s: %w", name, err)
		}
	}
	return nil
}
