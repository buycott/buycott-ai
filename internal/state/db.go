package state

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

func Open(artifactsPath string) (*sql.DB, error) {
	dir := filepath.Join(artifactsPath, ".buycott")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("creating state dir: %w", err)
	}

	dbPath := filepath.Join(dir, "state.db")
	db, err := sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("opening db: %w", err)
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrating db: %w", err)
	}

	return db, nil
}

// ClearAll removes all run data (tasks, events, releases, LLM logs, and the
// pipeline_state counters) while preserving the schema. Used to start a run
// over from scratch.
func ClearAll(db *sql.DB) error {
	tables := []string{"tasks", "events", "releases", "llm_logs", "pipeline_state"}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin reset tx: %w", err)
	}
	for _, t := range tables {
		if _, err := tx.Exec("DELETE FROM " + t); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("clearing %s: %w", t, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit reset tx: %w", err)
	}
	return nil
}

// WipeArtifacts deletes every entry under artifactsPath except the .buycott
// state directory (which holds the live SQLite DB). Used by a reset to discard
// all generated project files.
func WipeArtifacts(artifactsPath string) error {
	entries, err := os.ReadDir(artifactsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading artifacts dir: %w", err)
	}
	for _, e := range entries {
		if e.Name() == ".buycott" {
			continue
		}
		if err := os.RemoveAll(filepath.Join(artifactsPath, e.Name())); err != nil {
			return fmt.Errorf("removing %s: %w", e.Name(), err)
		}
	}
	return nil
}

func migrate(db *sql.DB) error {
	var version int
	_ = db.QueryRow("PRAGMA user_version").Scan(&version)

	if version < 1 {
		_, err := db.Exec(`
CREATE TABLE IF NOT EXISTS tasks (
    id                   TEXT PRIMARY KEY,
    title                TEXT NOT NULL,
    description          TEXT NOT NULL,
    acceptance_criteria  TEXT NOT NULL DEFAULT '[]',
    assigned_role        TEXT NOT NULL,
    status               TEXT NOT NULL DEFAULT 'pending',
    retry_count          INTEGER NOT NULL DEFAULT 0,
    parent_task_id       TEXT,
    conversation_history TEXT NOT NULL DEFAULT '[]',
    execution_results    TEXT NOT NULL DEFAULT '[]',
    created_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS events (
    id         TEXT PRIMARY KEY,
    type       TEXT NOT NULL,
    payload    TEXT NOT NULL DEFAULT '{}',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS pipeline_state (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS releases (
    id         TEXT PRIMARY KEY,
    version    TEXT NOT NULL UNIQUE,
    notes      TEXT NOT NULL,
    path       TEXT NOT NULL,
    created_at DATETIME NOT NULL
);

CREATE TABLE IF NOT EXISTS llm_logs (
    id             TEXT PRIMARY KEY,
    task_id        TEXT,
    role           TEXT NOT NULL,
    model          TEXT NOT NULL,
    call_type      TEXT NOT NULL,
    messages       TEXT NOT NULL DEFAULT '[]',
    response       TEXT NOT NULL DEFAULT '',
    input_tokens   INTEGER NOT NULL DEFAULT 0,
    output_tokens  INTEGER NOT NULL DEFAULT 0,
    duration_ms    INTEGER NOT NULL DEFAULT 0,
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS llm_logs_task_id  ON llm_logs(task_id);
CREATE INDEX IF NOT EXISTS llm_logs_role     ON llm_logs(role);
CREATE INDEX IF NOT EXISTS llm_logs_created  ON llm_logs(created_at);
`)
		if err != nil {
			return err
		}
		if _, err := db.Exec("PRAGMA user_version = 1"); err != nil {
			return err
		}
		version = 1
	}

	if version < 2 {
		// Add task dependency tracking, file rollback, and sub-task fields.
		stmts := []string{
			`ALTER TABLE tasks ADD COLUMN depends_on    TEXT NOT NULL DEFAULT '[]'`,
			`ALTER TABLE tasks ADD COLUMN files_written TEXT NOT NULL DEFAULT '[]'`,
			`ALTER TABLE tasks ADD COLUMN subtask_id    TEXT`,
		}
		for _, s := range stmts {
			if _, err := db.Exec(s); err != nil {
				return fmt.Errorf("schema v2: %w", err)
			}
		}
		if _, err := db.Exec("PRAGMA user_version = 2"); err != nil {
			return err
		}
	}

	return nil
}
