package state

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"

	"buycott/internal/model"
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

func TestClearAll(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Seed one row per data table.
	NewTaskStore(db).Save(&model.Task{ID: "t1", Title: "x", Status: model.StatusPending})
	NewEventStore(db).Append("seed", map[string]any{"a": 1})
	if _, err := db.Exec(`INSERT INTO pipeline_state(key,value) VALUES('k','v')`); err != nil {
		t.Fatal(err)
	}

	if err := ClearAll(db); err != nil {
		t.Fatalf("ClearAll: %v", err)
	}

	for _, tbl := range []string{"tasks", "events", "releases", "llm_logs", "pipeline_state"} {
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + tbl).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", tbl, err)
		}
		if n != 0 {
			t.Errorf("table %s not cleared: %d rows remain", tbl, n)
		}
	}
}

func TestWipeArtifacts(t *testing.T) {
	dir := t.TempDir()
	// .buycott (state) must survive; everything else must go.
	os.MkdirAll(filepath.Join(dir, ".buycott"), 0755)
	os.WriteFile(filepath.Join(dir, ".buycott", "state.db"), []byte("db"), 0644)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("pkg"), 0644)
	os.MkdirAll(filepath.Join(dir, "src"), 0755)
	os.WriteFile(filepath.Join(dir, "src", "app.js"), []byte("x"), 0644)

	if err := WipeArtifacts(dir); err != nil {
		t.Fatalf("WipeArtifacts: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, ".buycott", "state.db")); err != nil {
		t.Errorf("state db should be preserved: %v", err)
	}
	for _, p := range []string{"main.go", "src"} {
		if _, err := os.Stat(filepath.Join(dir, p)); !os.IsNotExist(err) {
			t.Errorf("%s should have been removed", p)
		}
	}
}
