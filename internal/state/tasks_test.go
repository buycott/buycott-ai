package state

import (
	"testing"
	"time"

	"buycott/internal/model"
)

func openTestDB(t *testing.T) (*TaskStore, *EventStore) {
	t.Helper()
	db, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewTaskStore(db), NewEventStore(db)
}

func makeTask(id, role string, status model.TaskStatus) *model.Task {
	now := time.Now()
	return &model.Task{
		ID:           id,
		Title:        "Task " + id,
		Description:  "desc",
		AssignedRole: role,
		Status:       status,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

func TestTaskStore_SaveAndGet(t *testing.T) {
	store, _ := openTestDB(t)

	task := makeTask("t1", "backend", model.StatusPending)
	task.AcceptanceCriteria = []string{"does the thing"}
	task.DependsOn = []string{"other-id"}
	task.FilesWritten = []string{"/artifacts/foo.go"}
	task.SubTaskID = "sub-1"

	if err := store.Save(task); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := store.Get("t1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Title != task.Title {
		t.Errorf("Title: got %q want %q", got.Title, task.Title)
	}
	if got.Status != model.StatusPending {
		t.Errorf("Status: got %q want pending", got.Status)
	}
	if len(got.AcceptanceCriteria) != 1 || got.AcceptanceCriteria[0] != "does the thing" {
		t.Errorf("AcceptanceCriteria: %v", got.AcceptanceCriteria)
	}
	if len(got.DependsOn) != 1 || got.DependsOn[0] != "other-id" {
		t.Errorf("DependsOn: %v", got.DependsOn)
	}
	if len(got.FilesWritten) != 1 || got.FilesWritten[0] != "/artifacts/foo.go" {
		t.Errorf("FilesWritten: %v", got.FilesWritten)
	}
	if got.SubTaskID != "sub-1" {
		t.Errorf("SubTaskID: got %q want sub-1", got.SubTaskID)
	}
}

func TestTaskStore_Get_NotFound(t *testing.T) {
	store, _ := openTestDB(t)
	got, err := store.Get("nonexistent")
	if err == nil {
		t.Fatalf("expected error for missing task, got %v", got)
	}
}

func TestTaskStore_UpdateViaUpsert(t *testing.T) {
	store, _ := openTestDB(t)

	task := makeTask("t1", "backend", model.StatusPending)
	store.Save(task)

	task.Status = model.StatusDone
	if err := store.Save(task); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, _ := store.Get("t1")
	if got.Status != model.StatusDone {
		t.Errorf("Status after update: got %q", got.Status)
	}
}

func TestTaskStore_FirstPending_NoDeps(t *testing.T) {
	store, _ := openTestDB(t)

	store.Save(makeTask("t1", "backend", model.StatusPending))
	store.Save(makeTask("t2", "frontend", model.StatusPending))

	got, err := store.FirstPending()
	if err != nil {
		t.Fatalf("FirstPending: %v", err)
	}
	if got == nil {
		t.Fatal("expected a task, got nil")
	}
	if got.ID != "t1" {
		t.Errorf("got %q, want t1", got.ID)
	}
}

func TestTaskStore_FirstPending_SkipsBlockedByDeps(t *testing.T) {
	store, _ := openTestDB(t)

	dep := makeTask("dep-1", "backend", model.StatusInProgress) // not done
	store.Save(dep)

	blocked := makeTask("blocked", "backend", model.StatusPending)
	blocked.DependsOn = []string{"dep-1"}
	store.Save(blocked)

	// Only task without blocked deps.
	free := makeTask("free", "backend", model.StatusPending)
	store.Save(free)

	got, err := store.FirstPending()
	if err != nil {
		t.Fatalf("FirstPending: %v", err)
	}
	if got == nil {
		t.Fatal("expected a task")
	}
	if got.ID != "free" {
		t.Errorf("got %q, want free", got.ID)
	}
}

func TestTaskStore_FirstPending_UnlocksWhenDepDone(t *testing.T) {
	store, _ := openTestDB(t)

	dep := makeTask("dep-1", "backend", model.StatusDone)
	store.Save(dep)

	blocked := makeTask("blocked", "backend", model.StatusPending)
	blocked.DependsOn = []string{"dep-1"}
	store.Save(blocked)

	got, err := store.FirstPending()
	if err != nil {
		t.Fatalf("FirstPending: %v", err)
	}
	if got == nil || got.ID != "blocked" {
		t.Errorf("got %v, want blocked", got)
	}
}

func TestTaskStore_FirstPending_NoneReady(t *testing.T) {
	store, _ := openTestDB(t)

	dep := makeTask("dep-1", "backend", model.StatusPending)
	store.Save(dep)

	blocked := makeTask("blocked", "backend", model.StatusPending)
	blocked.DependsOn = []string{"dep-1"}
	store.Save(blocked)

	// dep is pending (not done), blocked depends on it → neither ready.
	// dep has no deps so it should return dep.
	got, err := store.FirstPending()
	if err != nil {
		t.Fatalf("FirstPending: %v", err)
	}
	if got == nil || got.ID != "dep-1" {
		t.Errorf("expected dep-1 to be first ready, got %v", got)
	}
}

func TestTaskStore_FindWaitingForSubtask(t *testing.T) {
	store, _ := openTestDB(t)

	parent := makeTask("parent-1", "backend", model.StatusWaitingSubtask)
	parent.SubTaskID = "sub-1"
	store.Save(parent)

	// Non-waiting tasks with same subtask_id should not match.
	other := makeTask("other-1", "backend", model.StatusPending)
	other.SubTaskID = "sub-1"
	store.Save(other)

	got, err := store.FindWaitingForSubtask("sub-1")
	if err != nil {
		t.Fatalf("FindWaitingForSubtask: %v", err)
	}
	if got == nil {
		t.Fatal("expected parent, got nil")
	}
	if got.ID != "parent-1" {
		t.Errorf("got %q, want parent-1", got.ID)
	}
}

func TestTaskStore_FindWaitingForSubtask_NotFound(t *testing.T) {
	store, _ := openTestDB(t)

	got, err := store.FindWaitingForSubtask("nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestTaskStore_Stats(t *testing.T) {
	store, _ := openTestDB(t)

	store.Save(makeTask("t1", "backend", model.StatusPending))
	store.Save(makeTask("t2", "backend", model.StatusDone))
	store.Save(makeTask("t3", "frontend", model.StatusDone))

	stats, err := store.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats[model.StatusPending] != 1 {
		t.Errorf("pending: got %d, want 1", stats[model.StatusPending])
	}
	if stats[model.StatusDone] != 2 {
		t.Errorf("done: got %d, want 2", stats[model.StatusDone])
	}
}

func TestTaskStore_PipelineState(t *testing.T) {
	store, _ := openTestDB(t)

	if err := store.SetPipelineState("counter", "42"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := store.GetPipelineState("counter")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "42" {
		t.Errorf("got %q, want 42", got)
	}

	// Upsert — should update.
	store.SetPipelineState("counter", "99")
	got, _ = store.GetPipelineState("counter")
	if got != "99" {
		t.Errorf("after update got %q, want 99", got)
	}

	// Missing key returns empty string without error.
	got, err = store.GetPipelineState("missing")
	if err != nil {
		t.Fatalf("missing key error: %v", err)
	}
	if got != "" {
		t.Errorf("missing key: got %q, want empty", got)
	}
}

func TestTaskStore_List_StatusFilter(t *testing.T) {
	store, _ := openTestDB(t)

	store.Save(makeTask("t1", "backend", model.StatusPending))
	store.Save(makeTask("t2", "backend", model.StatusDone))
	store.Save(makeTask("t3", "frontend", model.StatusPending))

	pending, err := store.List(model.TaskFilter{Status: model.StatusPending})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(pending) != 2 {
		t.Errorf("pending count: got %d, want 2", len(pending))
	}
}
