package state

import (
	"testing"
	"time"

	"buycott/internal/model"
)

func openTestDBForLogs(t *testing.T) *LLMLogStore {
	t.Helper()
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewLLMLogStore(db)
}

func saveLog(t *testing.T, store *LLMLogStore, role, modelName string, in, out int) {
	t.Helper()
	err := store.Save(&model.LLMLog{
		ID:           role + "-" + modelName,
		Role:         role,
		Model:        modelName,
		CallType:     "process_task",
		InputTokens:  in,
		OutputTokens: out,
		CreatedAt:    time.Now(),
	})
	if err != nil {
		t.Fatalf("Save log: %v", err)
	}
}

func TestEstimateCost_KnownModels(t *testing.T) {
	tests := []struct {
		model string
		in    int64
		out   int64
		want  float64
	}{
		{"claude-opus-4-8", 1_000_000, 0, 5.00},
		{"claude-opus-4-8", 0, 1_000_000, 25.00},
		{"claude-sonnet-4-6", 1_000_000, 1_000_000, 18.00},
		{"anthropic/claude-sonnet-4-6", 1_000_000, 0, 3.00},
		{"openai/gpt-4o", 1_000_000, 0, 2.50},
		{"gemini/gemini-1.5-pro", 0, 1_000_000, 5.00},
		{"unknown-model", 1_000_000, 1_000_000, 0},
	}

	for _, tc := range tests {
		got := estimateCost(tc.model, tc.in, tc.out)
		if got != tc.want {
			t.Errorf("estimateCost(%q, %d, %d) = %v, want %v", tc.model, tc.in, tc.out, got, tc.want)
		}
	}
}

func TestTokenStats_Empty(t *testing.T) {
	store := openTestDBForLogs(t)
	stats, err := store.TokenStats()
	if err != nil {
		t.Fatalf("TokenStats: %v", err)
	}
	if stats != nil {
		t.Errorf("expected nil for empty store, got %v", stats)
	}
}

func TestTokenStats_Aggregation(t *testing.T) {
	store := openTestDBForLogs(t)

	// Two logs for backend/claude-opus-4-8.
	store.Save(&model.LLMLog{ID: "l1", Role: "backend", Model: "claude-opus-4-8",
		InputTokens: 1000, OutputTokens: 500, CreatedAt: time.Now()})
	store.Save(&model.LLMLog{ID: "l2", Role: "backend", Model: "claude-opus-4-8",
		InputTokens: 2000, OutputTokens: 300, CreatedAt: time.Now()})
	// One log for pm/claude-sonnet-4-6.
	store.Save(&model.LLMLog{ID: "l3", Role: "pm", Model: "claude-sonnet-4-6",
		InputTokens: 500, OutputTokens: 100, CreatedAt: time.Now()})

	stats, err := store.TokenStats()
	if err != nil {
		t.Fatalf("TokenStats: %v", err)
	}
	if len(stats) != 2 {
		t.Fatalf("expected 2 rows, got %d: %v", len(stats), stats)
	}

	// Find backend row.
	var backendRow, pmRow *RoleTokenStats
	for i := range stats {
		switch stats[i].Role {
		case "backend":
			backendRow = &stats[i]
		case "pm":
			pmRow = &stats[i]
		}
	}
	if backendRow == nil {
		t.Fatal("missing backend row")
	}
	if backendRow.InputTokens != 3000 {
		t.Errorf("backend input tokens: got %d, want 3000", backendRow.InputTokens)
	}
	if backendRow.OutputTokens != 800 {
		t.Errorf("backend output tokens: got %d, want 800", backendRow.OutputTokens)
	}
	if backendRow.Calls != 2 {
		t.Errorf("backend calls: got %d, want 2", backendRow.Calls)
	}
	if backendRow.EstCostUSD == 0 {
		t.Error("backend EstCostUSD should be nonzero")
	}

	if pmRow == nil {
		t.Fatal("missing pm row")
	}
	if pmRow.Calls != 1 {
		t.Errorf("pm calls: got %d, want 1", pmRow.Calls)
	}
}

func TestLLMLogStore_List_Filters(t *testing.T) {
	store := openTestDBForLogs(t)

	store.Save(&model.LLMLog{ID: "a1", TaskID: "task-1", Role: "backend", Model: "m", CreatedAt: time.Now()})
	store.Save(&model.LLMLog{ID: "a2", TaskID: "task-1", Role: "pm", Model: "m", CreatedAt: time.Now()})
	store.Save(&model.LLMLog{ID: "a3", TaskID: "task-2", Role: "backend", Model: "m", CreatedAt: time.Now()})

	// Filter by task_id.
	logs, err := store.List("task-1", "", 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(logs) != 2 {
		t.Errorf("task filter: got %d, want 2", len(logs))
	}

	// Filter by role.
	logs, err = store.List("", "backend", 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(logs) != 2 {
		t.Errorf("role filter: got %d, want 2", len(logs))
	}

	// Filter by both.
	logs, err = store.List("task-1", "backend", 0)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(logs) != 1 {
		t.Errorf("combined filter: got %d, want 1", len(logs))
	}

	// Limit.
	logs, err = store.List("", "", 1)
	if err != nil {
		t.Fatalf("List limit: %v", err)
	}
	if len(logs) != 1 {
		t.Errorf("limit: got %d, want 1", len(logs))
	}
}
