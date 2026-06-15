package pipeline

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"buycott/internal/executor"
	"buycott/internal/model"
	"buycott/internal/roles"
	"buycott/internal/state"
)

// ── mock implementations ──────────────────────────────────────────────────────

type mockPM struct {
	generateFn  func(ctx context.Context, direction string, state map[string]any) ([]*model.Task, error)
	reviewFn    func(ctx context.Context, task *model.Task, result model.ExecResult) (bool, string, error)
	checkRlsFn  func(ctx context.Context, state map[string]any) (bool, string, string, error)
	processCall int
}

func (m *mockPM) Name() string        { return "pm" }
func (m *mockPM) SystemPrompt() string { return "" }
func (m *mockPM) ProcessTask(ctx context.Context, task *model.Task) (roles.TaskOutput, error) {
	m.processCall++
	return roles.TaskOutput{Narrative: "done"}, nil
}
func (m *mockPM) GenerateTasks(ctx context.Context, direction string, state map[string]any) ([]*model.Task, error) {
	if m.generateFn != nil {
		return m.generateFn(ctx, direction, state)
	}
	return nil, nil
}
func (m *mockPM) ReviewTask(ctx context.Context, task *model.Task, result model.ExecResult) (bool, string, error) {
	if m.reviewFn != nil {
		return m.reviewFn(ctx, task, result)
	}
	return true, "approved", nil
}
func (m *mockPM) CheckRelease(ctx context.Context, state map[string]any) (bool, string, string, error) {
	if m.checkRlsFn != nil {
		return m.checkRlsFn(ctx, state)
	}
	return false, "", "", nil
}

type mockRole struct {
	roleName  string
	outputFn  func(ctx context.Context, task *model.Task) (roles.TaskOutput, error)
	callCount int
}

func (m *mockRole) Name() string        { return m.roleName }
func (m *mockRole) SystemPrompt() string { return "" }
func (m *mockRole) ProcessTask(ctx context.Context, task *model.Task) (roles.TaskOutput, error) {
	m.callCount++
	if m.outputFn != nil {
		return m.outputFn(ctx, task)
	}
	return roles.TaskOutput{Narrative: "done by " + m.roleName}, nil
}

type mockExecutor struct {
	result model.ExecResult
	err    error
}

func (m *mockExecutor) Run(_ context.Context, _ string, _ []string, _ string) (model.ExecResult, error) {
	return m.result, m.err
}

// ── test helpers ──────────────────────────────────────────────────────────────

func newTestPipeline(t *testing.T, pm roles.PMRole, registry *roles.Registry, exec executor.Executor) (*Pipeline, *state.TaskStore, *state.EventStore, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := state.Open(dir)
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	tasks := state.NewTaskStore(db)
	events := state.NewEventStore(db)
	releases := state.NewReleaseStore(db)

	if exec == nil {
		exec = &mockExecutor{result: model.ExecResult{ExitCode: 0}}
	}

	p := New(tasks, events, releases, registry, pm, nil, nil, nil, exec,
		dir, "test direction", 3, 0, 10*time.Second, 2, 20)
	return p, tasks, events, dir
}

func saveTask(t *testing.T, store *state.TaskStore, id, role string, status model.TaskStatus) *model.Task {
	t.Helper()
	task := &model.Task{
		ID:           id,
		Title:        "Task " + id,
		Description:  "Test task",
		AssignedRole: role,
		Status:       status,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := store.Save(task); err != nil {
		t.Fatalf("Save task: %v", err)
	}
	return task
}

// ── context trimming ──────────────────────────────────────────────────────────

func TestContextTrimming_KeepsFirstAndRecent(t *testing.T) {
	pm := &mockPM{reviewFn: func(_ context.Context, _ *model.Task, _ model.ExecResult) (bool, string, error) {
		return true, "ok", nil
	}}
	registry := roles.NewRegistry()

	capturedHistory := []model.Message{}
	eng := &mockRole{
		roleName: "backend",
		outputFn: func(_ context.Context, task *model.Task) (roles.TaskOutput, error) {
			capturedHistory = make([]model.Message, len(task.ConversationHistory))
			copy(capturedHistory, task.ConversationHistory)
			return roles.TaskOutput{Narrative: "done"}, nil
		},
	}
	registry.Register(eng)
	registry.Register(pm)

	p, tasks, _, _ := newTestPipeline(t, pm, registry, nil)
	p.maxHistoryMsgs = 4 // keep first + last 3

	task := &model.Task{
		ID:           "trim-test",
		Title:        "Trimming task",
		Description:  "test",
		AssignedRole: "backend",
		Status:       model.StatusPending,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	// Build up a long history: first message + 10 more.
	task.ConversationHistory = []model.Message{
		{Role: "user", Content: "initial context"},
	}
	for i := 1; i <= 10; i++ {
		role := "user"
		if i%2 == 0 {
			role = "assistant"
		}
		task.ConversationHistory = append(task.ConversationHistory,
			model.Message{Role: role, Content: "message " + string(rune('0'+i))},
		)
	}
	tasks.Save(task)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p.processTask(ctx, task)

	// After trimming, ProcessTask should have seen at most maxHistoryMsgs messages.
	// First message must be the initial context.
	if len(capturedHistory) > p.maxHistoryMsgs {
		t.Errorf("history not trimmed: got %d messages, want <= %d", len(capturedHistory), p.maxHistoryMsgs)
	}
	if len(capturedHistory) > 0 && capturedHistory[0].Content != "initial context" {
		t.Errorf("first message changed: got %q", capturedHistory[0].Content)
	}
}

// ── sub-task lifecycle ────────────────────────────────────────────────────────

func TestSubTask_SpawnAndResume(t *testing.T) {
	pm := &mockPM{}
	registry := roles.NewRegistry()

	callCount := 0
	eng := &mockRole{
		roleName: "backend",
		outputFn: func(_ context.Context, task *model.Task) (roles.TaskOutput, error) {
			callCount++
			if callCount == 1 {
				// First call: request a sub-task.
				return roles.TaskOutput{
					Narrative: "need help",
					SubTask: &model.SubTaskRequest{
						Role:        "frontend",
						Title:       "Build UI",
						Description: "Create the page",
					},
				}, nil
			}
			// Second call (after sub-task resumes): normal completion.
			return roles.TaskOutput{Narrative: "all done"}, nil
		},
	}
	registry.Register(eng)
	registry.Register(pm)

	p, tasks, _, _ := newTestPipeline(t, pm, registry, nil)

	task := saveTask(t, tasks, "parent-1", "backend", model.StatusPending)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// First processTask call — should spawn sub-task and pause parent.
	p.processTask(ctx, task)

	// Verify parent is waiting.
	parent, _ := tasks.Get("parent-1")
	if parent.Status != model.StatusWaitingSubtask {
		t.Errorf("parent status: got %q, want waiting_subtask", parent.Status)
	}
	if parent.SubTaskID == "" {
		t.Error("parent should have SubTaskID set")
	}

	// Find the spawned sub-task.
	all, _ := tasks.List(model.TaskFilter{})
	var subTask *model.Task
	for _, t2 := range all {
		if t2.ID != "parent-1" {
			subTask = t2
		}
	}
	if subTask == nil {
		t.Fatal("sub-task not found")
	}
	if subTask.AssignedRole != "frontend" {
		t.Errorf("subtask role: %q", subTask.AssignedRole)
	}
	if subTask.ParentTaskID != "parent-1" {
		t.Errorf("subtask parent: %q", subTask.ParentTaskID)
	}

	// Simulate sub-task completion.
	subTask.Status = model.StatusDone
	subTask.ConversationHistory = []model.Message{
		{Role: "assistant", Content: "UI complete"},
	}
	subTask.UpdatedAt = time.Now()
	tasks.Save(subTask)

	// resumeWaitingParent should re-queue parent with sub-task result.
	p.resumeWaitingParent(ctx, subTask)

	parent, _ = tasks.Get("parent-1")
	if parent.Status != model.StatusPending {
		t.Errorf("parent status after resume: got %q, want pending", parent.Status)
	}
	if parent.SubTaskID != "" {
		t.Error("parent SubTaskID should be cleared after resume")
	}

	// Check that sub-task result was injected into history.
	found := false
	for _, msg := range parent.ConversationHistory {
		if msg.Role == "user" {
			found = true
			break
		}
	}
	if !found {
		t.Error("sub-task result not injected into parent history")
	}
}

// ── file rollback on escalation ───────────────────────────────────────────────

func TestEscalate_DeletesFilesWritten(t *testing.T) {
	dir := t.TempDir()
	db, err := state.Open(dir)
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer db.Close()

	tasks := state.NewTaskStore(db)
	events := state.NewEventStore(db)
	releases := state.NewReleaseStore(db)
	registry := roles.NewRegistry()
	pm := &mockPM{}
	registry.Register(pm)

	p := New(tasks, events, releases, registry, pm, nil, nil, nil,
		&mockExecutor{}, dir, "dir", 1, 0, 5*time.Second, 2, 20)

	// Create a real file that escalation should remove.
	filePath := filepath.Join(dir, "should-be-deleted.txt")
	os.WriteFile(filePath, []byte("content"), 0644)

	task := &model.Task{
		ID:           "esc-1",
		Title:        "Escalating task",
		Description:  "test",
		AssignedRole: "backend",
		Status:       model.StatusInProgress,
		FilesWritten: []string{filePath},
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	tasks.Save(task)

	if err := p.escalate(task, "too many failures"); err != nil {
		t.Fatalf("escalate: %v", err)
	}

	// File should be gone.
	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Error("escalated file should have been deleted")
	}

	// Task should be escalated.
	got, _ := tasks.Get("esc-1")
	if got.Status != model.StatusEscalated {
		t.Errorf("task status: got %q, want escalated", got.Status)
	}

	// An escalation PM task should exist.
	all, _ := tasks.List(model.TaskFilter{Status: model.StatusPending})
	if len(all) == 0 {
		t.Error("escalation PM task should have been created")
	}
	if all[0].AssignedRole != "pm" {
		t.Errorf("escalation task role: %q", all[0].AssignedRole)
	}
}

func TestEscalate_NonExistentFileIsOK(t *testing.T) {
	dir := t.TempDir()
	db, _ := state.Open(dir)
	defer db.Close()

	tasks := state.NewTaskStore(db)
	events := state.NewEventStore(db)
	releases := state.NewReleaseStore(db)
	pm := &mockPM{}
	registry := roles.NewRegistry()
	registry.Register(pm)

	p := New(tasks, events, releases, registry, pm, nil, nil, nil,
		&mockExecutor{}, dir, "dir", 1, 0, 5*time.Second, 2, 20)

	task := &model.Task{
		ID:           "esc-2",
		Title:        "test",
		Description:  "d",
		AssignedRole: "backend",
		Status:       model.StatusInProgress,
		FilesWritten: []string{"/nonexistent/path/file.txt"},
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	tasks.Save(task)

	// Should not return an error even though the file doesn't exist.
	if err := p.escalate(task, "reason"); err != nil {
		t.Fatalf("escalate with nonexistent file: %v", err)
	}
}

// ── generateTasks (proactive refill) ─────────────────────────────────────────

func TestGenerateTasks_SavesReturnedTasks(t *testing.T) {
	generated := []*model.Task{
		{Title: "Generated A", Description: "d", AssignedRole: "backend"},
		{Title: "Generated B", Description: "d", AssignedRole: "frontend"},
	}
	pm := &mockPM{
		generateFn: func(_ context.Context, _ string, _ map[string]any) ([]*model.Task, error) {
			return generated, nil
		},
	}
	registry := roles.NewRegistry()
	registry.Register(pm)

	p, tasks, _, _ := newTestPipeline(t, pm, registry, nil)

	ctx := context.Background()
	if err := p.generateTasks(ctx); err != nil {
		t.Fatalf("generateTasks: %v", err)
	}

	all, _ := tasks.List(model.TaskFilter{Status: model.StatusPending})
	if len(all) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(all))
	}
	titles := map[string]bool{}
	for _, task := range all {
		titles[task.Title] = true
	}
	if !titles["Generated A"] || !titles["Generated B"] {
		t.Errorf("unexpected task titles: %v", titles)
	}
}

// ── buildFileTree ─────────────────────────────────────────────────────────────

func TestBuildFileTree_Basic(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "src"), 0755)
	os.WriteFile(filepath.Join(dir, "src", "main.go"), []byte("x"), 0644)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("y"), 0644)
	os.MkdirAll(filepath.Join(dir, ".hidden"), 0755)
	os.WriteFile(filepath.Join(dir, ".hidden", "secret"), []byte("z"), 0644)

	tree := buildFileTree(dir, 4, 100)

	if tree == "" {
		t.Error("expected non-empty tree")
	}
	// Should include visible files.
	if !containsStr(tree, "README.md") {
		t.Errorf("tree missing README.md:\n%s", tree)
	}
	if !containsStr(tree, "main.go") {
		t.Errorf("tree missing main.go:\n%s", tree)
	}
	// Hidden directories should be skipped.
	if containsStr(tree, ".hidden") {
		t.Errorf("tree should not include .hidden:\n%s", tree)
	}
}

func TestBuildFileTree_MaxEntries(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 10; i++ {
		os.WriteFile(filepath.Join(dir, string(rune('a'+i))+".txt"), []byte("x"), 0644)
	}

	tree := buildFileTree(dir, 4, 3)
	lines := 0
	for _, ch := range tree {
		if ch == '\n' {
			lines++
		}
	}
	// With maxEntries=3, we expect at most a few lines (some may include "...").
	if lines > 5 {
		t.Errorf("tree has too many lines (%d) for maxEntries=3", lines)
	}
}

func TestBuildFileTree_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	tree := buildFileTree(dir, 4, 100)
	if tree != "" {
		t.Errorf("expected empty tree for empty dir, got: %q", tree)
	}
}

// ── handleFailure ─────────────────────────────────────────────────────────────

func TestHandleFailure_RetryBelowMax(t *testing.T) {
	dir := t.TempDir()
	db, _ := state.Open(dir)
	defer db.Close()

	tasks := state.NewTaskStore(db)
	events := state.NewEventStore(db)
	releases := state.NewReleaseStore(db)
	pm := &mockPM{}
	registry := roles.NewRegistry()
	registry.Register(pm)

	p := New(tasks, events, releases, registry, pm, nil, nil, nil,
		&mockExecutor{}, dir, "d", 3, 0, 5*time.Second, 2, 20)

	task := &model.Task{
		ID:           "fail-1",
		Title:        "t",
		Description:  "d",
		AssignedRole: "backend",
		Status:       model.StatusInProgress,
		RetryCount:   0,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	tasks.Save(task)

	if err := p.handleFailure(context.Background(), task, "oops", model.ExecResult{}); err != nil {
		t.Fatalf("handleFailure: %v", err)
	}

	got, _ := tasks.Get("fail-1")
	if got.Status != model.StatusPending {
		t.Errorf("status: got %q, want pending", got.Status)
	}
	if got.RetryCount != 1 {
		t.Errorf("retry count: got %d, want 1", got.RetryCount)
	}
}

func TestHandleFailure_EscalatesAtMax(t *testing.T) {
	dir := t.TempDir()
	db, _ := state.Open(dir)
	defer db.Close()

	tasks := state.NewTaskStore(db)
	events := state.NewEventStore(db)
	releases := state.NewReleaseStore(db)
	pm := &mockPM{}
	registry := roles.NewRegistry()
	registry.Register(pm)

	p := New(tasks, events, releases, registry, pm, nil, nil, nil,
		&mockExecutor{}, dir, "d", 3, 0, 5*time.Second, 2, 20)

	task := &model.Task{
		ID:           "max-retry",
		Title:        "t",
		Description:  "d",
		AssignedRole: "backend",
		Status:       model.StatusInProgress,
		RetryCount:   2, // next failure → 3 = maxRetry
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	tasks.Save(task)

	if err := p.handleFailure(context.Background(), task, "final failure", model.ExecResult{}); err != nil {
		t.Fatalf("handleFailure at max: %v", err)
	}

	got, _ := tasks.Get("max-retry")
	if got.Status != model.StatusEscalated {
		t.Errorf("status: got %q, want escalated", got.Status)
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func containsStr(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstring(s, sub))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
