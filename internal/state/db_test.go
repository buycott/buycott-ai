package state

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestOpen_CreatesSchema(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	tables := []string{"tasks", "events", "pipeline_state", "releases", "llm_logs"}
	for _, tbl := range tables {
		var name string
		err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&name)
		if err != nil {
			t.Errorf("table %q missing: %v", tbl, err)
		}
	}
}

func TestMigration_V2AddsColumns(t *testing.T) {
	dir := t.TempDir()
	stateDir := filepath.Join(dir, ".buycott")
	os.MkdirAll(stateDir, 0755)

	// Build a v1-style DB by hand (no v2 columns, user_version=1).
	v1path := filepath.Join(stateDir, "state.db") + "?_journal_mode=WAL"
	db, err := sql.Open("sqlite", v1path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE tasks (
		id TEXT PRIMARY KEY, title TEXT NOT NULL, description TEXT NOT NULL,
		acceptance_criteria TEXT NOT NULL DEFAULT '[]', assigned_role TEXT NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending', retry_count INTEGER NOT NULL DEFAULT 0,
		parent_task_id TEXT, conversation_history TEXT NOT NULL DEFAULT '[]',
		execution_results TEXT NOT NULL DEFAULT '[]',
		created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`)
	if err != nil {
		t.Fatal(err)
	}
	db.Exec("PRAGMA user_version = 1")
	db.Close()

	// Open via our code — should apply v2 migration (ALTER TABLE).
	db2, err := Open(dir)
	if err != nil {
		t.Fatalf("Open after v1: %v", err)
	}
	defer db2.Close()

	// Verify v2 columns exist.
	_, err = db2.Exec("SELECT depends_on, files_written, subtask_id FROM tasks LIMIT 0")
	if err != nil {
		t.Fatalf("v2 columns missing after migration: %v", err)
	}
}

func TestMigration_Idempotent(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	// Re-opening should not fail.
	db2, err := Open(dir)
	if err != nil {
		t.Fatalf("second Open failed: %v", err)
	}
	db2.Close()
}
