package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Script CRUD on the PCStore. Scripts are user-authored JavaScript payload
// transformations (see internal/scripting); the store is a plain persistence
// layer and never evaluates them. Callers pass the current time explicitly so
// the store keeps no clock dependency, matching the rest of PCStore.

// InsertScript appends a script and returns its new id. CreatedAt/UpdatedAt
// are both set to now (the ScriptRow's own timestamp fields are ignored).
func (s *PCStore) InsertScript(ctx context.Context, sc ScriptRow, now time.Time) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO scripts (name, "trigger", body, priority, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		sc.Name, sc.Trigger, sc.Body, sc.Priority, boolToInt(sc.Enabled), now.Unix(), now.Unix(),
	)
	if err != nil {
		return 0, fmt.Errorf("PCStore.InsertScript %q: %w", sc.Name, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("PCStore.InsertScript %q last id: %w", sc.Name, err)
	}
	return id, nil
}

// UpdateScript overwrites the mutable fields (name, trigger, body, priority,
// enabled) of the script identified by sc.ID and bumps updated_at to now.
// Returns ErrNotFound if no row has that id.
func (s *PCStore) UpdateScript(ctx context.Context, sc ScriptRow, now time.Time) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE scripts
		 SET name = ?, "trigger" = ?, body = ?, priority = ?, enabled = ?, updated_at = ?
		 WHERE id = ?`,
		sc.Name, sc.Trigger, sc.Body, sc.Priority, boolToInt(sc.Enabled), now.Unix(), sc.ID,
	)
	if err != nil {
		return fmt.Errorf("PCStore.UpdateScript %d: %w", sc.ID, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("PCStore.UpdateScript %d: %w", sc.ID, ErrNotFound)
	}
	return nil
}

// SetScriptEnabled toggles the enabled flag on one script (the common sidebar
// action). Returns ErrNotFound if no row has that id.
func (s *PCStore) SetScriptEnabled(ctx context.Context, id int64, enabled bool, now time.Time) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE scripts SET enabled = ?, updated_at = ? WHERE id = ?`,
		boolToInt(enabled), now.Unix(), id,
	)
	if err != nil {
		return fmt.Errorf("PCStore.SetScriptEnabled %d: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("PCStore.SetScriptEnabled %d: %w", id, ErrNotFound)
	}
	return nil
}

// DeleteScript removes a script by id. Returns ErrNotFound if no row matched.
func (s *PCStore) DeleteScript(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM scripts WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("PCStore.DeleteScript %d: %w", id, err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("PCStore.DeleteScript %d: %w", id, ErrNotFound)
	}
	return nil
}

// ScriptByID returns a single script. Returns ErrNotFound if none matched.
func (s *PCStore) ScriptByID(ctx context.Context, id int64) (*ScriptRow, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, name, "trigger", body, priority, enabled, created_at, updated_at
		 FROM scripts WHERE id = ?`, id,
	)
	sc, err := scanScript(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("PCStore.ScriptByID %d: %w", id, ErrNotFound)
		}
		return nil, fmt.Errorf("PCStore.ScriptByID %d: %w", id, err)
	}
	return sc, nil
}

// Scripts lists every script, ordered by trigger then priority then id so the
// GUI sidebar shows a stable grouping.
func (s *PCStore) Scripts(ctx context.Context) ([]ScriptRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, "trigger", body, priority, enabled, created_at, updated_at
		 FROM scripts ORDER BY "trigger", priority, id`,
	)
	if err != nil {
		return nil, fmt.Errorf("PCStore.Scripts: %w", err)
	}
	defer rows.Close()
	return collectScripts(rows)
}

// ScriptSummaries lists script metadata without loading JavaScript bodies.
func (s *PCStore) ScriptSummaries(ctx context.Context) ([]ScriptRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, name, "trigger", '', priority, enabled, created_at, updated_at
		 FROM scripts ORDER BY "trigger", priority, id`,
	)
	if err != nil {
		return nil, fmt.Errorf("PCStore.ScriptSummaries: %w", err)
	}
	defer rows.Close()
	return collectScripts(rows)
}

// ScriptsByTrigger lists scripts for one trigger in priority order. When
// enabledOnly is true, disabled scripts are excluded — this is what the
// scripting chain runner loads before an interception/replay/webhook hook.
func (s *PCStore) ScriptsByTrigger(ctx context.Context, trigger string, enabledOnly bool) ([]ScriptRow, error) {
	q := `SELECT id, name, "trigger", body, priority, enabled, created_at, updated_at
	      FROM scripts WHERE "trigger" = ?`
	if enabledOnly {
		q += ` AND enabled = 1`
	}
	q += ` ORDER BY priority, id`
	rows, err := s.db.QueryContext(ctx, q, trigger)
	if err != nil {
		return nil, fmt.Errorf("PCStore.ScriptsByTrigger %q: %w", trigger, err)
	}
	defer rows.Close()
	return collectScripts(rows)
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type scanner interface {
	Scan(dest ...any) error
}

// scanScript reads one scripts row from a *sql.Row or *sql.Rows.
func scanScript(sc scanner) (*ScriptRow, error) {
	var (
		r       ScriptRow
		enabled int64
		created int64
		updated int64
	)
	if err := sc.Scan(&r.ID, &r.Name, &r.Trigger, &r.Body, &r.Priority, &enabled, &created, &updated); err != nil {
		return nil, err
	}
	r.Enabled = enabled != 0
	r.CreatedAt = time.Unix(created, 0).UTC()
	r.UpdatedAt = time.Unix(updated, 0).UTC()
	return &r, nil
}

// collectScripts drains rows into a slice.
func collectScripts(rows *sql.Rows) ([]ScriptRow, error) {
	var out []ScriptRow
	for rows.Next() {
		sc, err := scanScript(rows)
		if err != nil {
			return nil, fmt.Errorf("PCStore scripts scan: %w", err)
		}
		out = append(out, *sc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("PCStore scripts rows: %w", err)
	}
	return out, nil
}

// boolToInt maps a Go bool to SQLite's 0/1 integer boolean.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
