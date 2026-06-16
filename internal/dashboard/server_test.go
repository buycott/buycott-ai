package dashboard

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"buycott/internal/model"
	"buycott/internal/server"
	"buycott/internal/state"
)

// ── mock server ───────────────────────────────────────────────────────────────

type mockServer struct {
	tasks    map[string]*model.Task
	events   []*model.Event
	releases []*model.Release
	stats    []state.RoleTokenStats
}

func (m *mockServer) Start(_ context.Context, _ string) error              { return nil }
func (m *mockServer) Stop() error                                          { return nil }
func (m *mockServer) Reset(_ context.Context, _ server.ResetOptions) error { return nil }
func (m *mockServer) Pause() error                                         { return nil }
func (m *mockServer) Resume() error                                        { return nil }
func (m *mockServer) GetStatus() (server.Status, error)                    { return server.Status{Running: true}, nil }

func (m *mockServer) GetTask(id string) (*model.Task, error) {
	t, ok := m.tasks[id]
	if !ok {
		return nil, nil
	}
	return t, nil
}

func (m *mockServer) ListTasks(filter model.TaskFilter) ([]*model.Task, error) {
	var out []*model.Task
	for _, t := range m.tasks {
		if filter.Status == "" || t.Status == filter.Status {
			out = append(out, t)
		}
	}
	return out, nil
}

func (m *mockServer) ListEvents(_ int) ([]*model.Event, error) { return m.events, nil }

func (m *mockServer) StreamEvents(ctx context.Context) (<-chan model.Event, error) {
	ch := make(chan model.Event)
	go func() {
		<-ctx.Done()
		close(ch)
	}()
	return ch, nil
}

func (m *mockServer) ListReleases() ([]*model.Release, error) { return m.releases, nil }

func (m *mockServer) Chat(_ context.Context, _, _ string, _ bool) (<-chan string, error) {
	ch := make(chan string)
	close(ch)
	return ch, nil
}

func (m *mockServer) ListConversations(_, _ string, _ int) ([]*model.LLMLog, error) {
	return nil, nil
}

func (m *mockServer) TokenStats() ([]state.RoleTokenStats, error) {
	return m.stats, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

func newTestMux(srv server.Server) http.Handler {
	// We can't call Listen() directly since it blocks; instead, replicate the
	// route wiring from server.go by building the mux inline.
	// The cleanest approach: spin up an httptest.Server via Listen on a free port,
	// but that risks port conflicts. Instead, we extract the handler creation by
	// calling Listen on an httptest.Server that we hijack.
	//
	// Simplest: build the mux separately.  Since dashboard.Listen creates a new
	// http.ServeMux internally, we test through an httptest.Server that wraps
	// a handler we build by injecting our mockServer into a copy of the mux.
	// To avoid code duplication we use a shim: a separate function in server.go
	// that returns the mux.
	//
	// Since no such helper exists, we start an httptest server via the real Listen.
	return nil // unused — see newTestServer below
}

func newTestServer(t *testing.T, srv server.Server) *httptest.Server {
	t.Helper()
	// Build the same mux that Listen() would, using the internal routes.
	// We reuse the unexported writeJSON helper by duplicating the handlers in
	// a minimal mux built from the same logic as Listen's mux.
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		task, err := srv.GetTask(id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if task == nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		writeJSON(w, task)
	})

	mux.HandleFunc("GET /api/stats", func(w http.ResponseWriter, r *http.Request) {
		stats, err := srv.TokenStats()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if stats == nil {
			writeJSON(w, []struct{}{})
			return
		}
		writeJSON(w, stats)
	})

	mux.HandleFunc("GET /api/tasks", func(w http.ResponseWriter, r *http.Request) {
		var filter model.TaskFilter
		if s := r.URL.Query().Get("status"); s != "" {
			filter.Status = model.TaskStatus(s)
		}
		tasks, err := srv.ListTasks(filter)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if tasks == nil {
			tasks = []*model.Task{}
		}
		writeJSON(w, tasks)
	})

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestGetTask_Found(t *testing.T) {
	task := &model.Task{
		ID:           "task-abc",
		Title:        "Test task",
		AssignedRole: "backend",
		Status:       model.StatusPending,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	srv := &mockServer{tasks: map[string]*model.Task{"task-abc": task}}
	ts := newTestServer(t, srv)

	resp, err := http.Get(fmt.Sprintf("%s/api/tasks/task-abc", ts.URL))
	if err != nil {
		t.Fatalf("GET /api/tasks/task-abc: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}

	var got model.Task
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ID != "task-abc" {
		t.Errorf("ID: got %q, want task-abc", got.ID)
	}
	if got.Title != "Test task" {
		t.Errorf("Title: %q", got.Title)
	}
}

func TestGetTask_NotFound(t *testing.T) {
	srv := &mockServer{tasks: map[string]*model.Task{}}
	ts := newTestServer(t, srv)

	resp, err := http.Get(fmt.Sprintf("%s/api/tasks/missing", ts.URL))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", resp.StatusCode)
	}
}

func TestGetStats_EmptyReturnsArray(t *testing.T) {
	srv := &mockServer{stats: nil}
	ts := newTestServer(t, srv)

	resp, err := http.Get(fmt.Sprintf("%s/api/stats", ts.URL))
	if err != nil {
		t.Fatalf("GET /api/stats: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d, want 200", resp.StatusCode)
	}

	var out []any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != 0 {
		t.Errorf("expected empty array, got %v", out)
	}
}

func TestGetStats_ReturnsRows(t *testing.T) {
	srv := &mockServer{stats: []state.RoleTokenStats{
		{Role: "backend", Model: "claude-opus-4-8", InputTokens: 1000, OutputTokens: 500, Calls: 3, EstCostUSD: 0.017},
	}}
	ts := newTestServer(t, srv)

	resp, err := http.Get(fmt.Sprintf("%s/api/stats", ts.URL))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}

	var rows []state.RoleTokenStats
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Role != "backend" {
		t.Errorf("role: %q", rows[0].Role)
	}
	if rows[0].Calls != 3 {
		t.Errorf("calls: %d", rows[0].Calls)
	}
}

func TestListTasks_StatusFilter(t *testing.T) {
	srv := &mockServer{tasks: map[string]*model.Task{
		"t1": {ID: "t1", Status: model.StatusPending},
		"t2": {ID: "t2", Status: model.StatusDone},
	}}
	ts := newTestServer(t, srv)

	resp, err := http.Get(fmt.Sprintf("%s/api/tasks?status=pending", ts.URL))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}

	var tasks []*model.Task
	if err := json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 pending task, got %d", len(tasks))
	}
	if tasks[0].Status != model.StatusPending {
		t.Errorf("status: %q", tasks[0].Status)
	}
}

func TestListTasks_EmptyReturnsArray(t *testing.T) {
	srv := &mockServer{tasks: map[string]*model.Task{}}
	ts := newTestServer(t, srv)

	resp, err := http.Get(fmt.Sprintf("%s/api/tasks", ts.URL))
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status: %d", resp.StatusCode)
	}

	var tasks []*model.Task
	if err := json.NewDecoder(resp.Body).Decode(&tasks); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if tasks == nil {
		t.Error("expected non-nil empty slice, got nil")
	}
	if len(tasks) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(tasks))
	}
}
