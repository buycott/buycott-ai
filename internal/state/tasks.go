package state

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"buycott/internal/model"
)

type TaskStore struct {
	db *sql.DB
}

func NewTaskStore(db *sql.DB) *TaskStore {
	return &TaskStore{db: db}
}

func (s *TaskStore) Save(t *model.Task) error {
	criteria, _ := json.Marshal(t.AcceptanceCriteria)
	history, _ := json.Marshal(t.ConversationHistory)
	results, _ := json.Marshal(t.ExecutionResults)
	dependsOn, _ := json.Marshal(t.DependsOn)
	filesWritten, _ := json.Marshal(t.FilesWritten)

	var parentID any
	if t.ParentTaskID != "" {
		parentID = t.ParentTaskID
	}
	var subtaskID any
	if t.SubTaskID != "" {
		subtaskID = t.SubTaskID
	}

	_, err := s.db.Exec(`
		INSERT INTO tasks
			(id, title, description, acceptance_criteria, assigned_role, status,
			 retry_count, parent_task_id, depends_on, files_written, subtask_id,
			 conversation_history, execution_results, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET
			title=excluded.title,
			description=excluded.description,
			acceptance_criteria=excluded.acceptance_criteria,
			assigned_role=excluded.assigned_role,
			status=excluded.status,
			retry_count=excluded.retry_count,
			parent_task_id=excluded.parent_task_id,
			depends_on=excluded.depends_on,
			files_written=excluded.files_written,
			subtask_id=excluded.subtask_id,
			conversation_history=excluded.conversation_history,
			execution_results=excluded.execution_results,
			updated_at=excluded.updated_at`,
		t.ID, t.Title, t.Description, string(criteria), t.AssignedRole,
		string(t.Status), t.RetryCount, parentID,
		string(dependsOn), string(filesWritten), subtaskID,
		string(history), string(results),
		t.CreatedAt.UTC().Format(time.RFC3339),
		t.UpdatedAt.UTC().Format(time.RFC3339),
	)
	return err
}

func (s *TaskStore) Get(id string) (*model.Task, error) {
	row := s.db.QueryRow(selectCols+` FROM tasks WHERE id = ?`, id)
	return scanTask(row)
}

// FirstPending returns the oldest pending task whose dependencies are all done.
func (s *TaskStore) FirstPending() (*model.Task, error) {
	rows, err := s.db.Query(selectCols + ` FROM tasks WHERE status = 'pending' ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		if len(t.DependsOn) == 0 {
			return t, nil
		}
		ready, err := s.allDepsDone(t.DependsOn)
		if err != nil {
			return nil, err
		}
		if ready {
			return t, nil
		}
	}
	return nil, rows.Err()
}

func (s *TaskStore) allDepsDone(ids []string) (bool, error) {
	for _, id := range ids {
		var status string
		err := s.db.QueryRow(`SELECT status FROM tasks WHERE id = ?`, id).Scan(&status)
		if err == sql.ErrNoRows {
			return false, nil // dep doesn't exist yet
		}
		if err != nil {
			return false, err
		}
		if status != string(model.StatusDone) {
			return false, nil
		}
	}
	return true, nil
}

// FindWaitingForSubtask returns the task (if any) that is blocked on subtaskID.
func (s *TaskStore) FindWaitingForSubtask(subtaskID string) (*model.Task, error) {
	row := s.db.QueryRow(selectCols+` FROM tasks WHERE subtask_id = ? AND status = 'waiting_subtask'`, subtaskID)
	t, err := scanTask(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return t, err
}

func (s *TaskStore) List(filter model.TaskFilter) ([]*model.Task, error) {
	q := selectCols + ` FROM tasks`
	var args []any
	if filter.Status != "" {
		q += " WHERE status = ?"
		args = append(args, string(filter.Status))
	}
	q += " ORDER BY created_at ASC"

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []*model.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (s *TaskStore) ListDoneSince(after time.Time) ([]*model.Task, error) {
	rows, err := s.db.Query(
		selectCols+` FROM tasks WHERE status = 'done' AND updated_at > ? ORDER BY updated_at ASC`,
		after.UTC().Format(time.RFC3339),
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tasks []*model.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// RecentDone returns the most recently completed tasks, newest first.
func (s *TaskStore) RecentDone(limit int) ([]*model.Task, error) {
	rows, err := s.db.Query(selectCols+` FROM tasks WHERE status = 'done' ORDER BY updated_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var tasks []*model.Task
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

func (s *TaskStore) CountByStatus(status model.TaskStatus) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE status = ?`, string(status)).Scan(&n)
	return n, err
}

func (s *TaskStore) Stats() (map[model.TaskStatus]int, error) {
	rows, err := s.db.Query(`SELECT status, COUNT(*) FROM tasks GROUP BY status`)
	if err != nil {
		return nil, fmt.Errorf("stats query: %w", err)
	}
	defer rows.Close()

	stats := make(map[model.TaskStatus]int)
	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return nil, err
		}
		stats[model.TaskStatus(status)] = count
	}
	return stats, rows.Err()
}

func (s *TaskStore) SetPipelineState(key, value string) error {
	_, err := s.db.Exec(`
		INSERT INTO pipeline_state (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

func (s *TaskStore) GetPipelineState(key string) (string, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM pipeline_state WHERE key = ?`, key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// selectCols is the column list for all task SELECT queries.
const selectCols = `SELECT id, title, description, acceptance_criteria, assigned_role, status,
       retry_count, parent_task_id, depends_on, files_written, subtask_id,
       conversation_history, execution_results, created_at, updated_at`

type dbScanner interface {
	Scan(dest ...any) error
}

func scanTask(sc dbScanner) (*model.Task, error) {
	var t model.Task
	var criteriaJSON, historyJSON, resultsJSON, dependsOnJSON, filesWrittenJSON string
	var parentID, subtaskID sql.NullString
	var createdAt, updatedAt string

	err := sc.Scan(
		&t.ID, &t.Title, &t.Description, &criteriaJSON,
		&t.AssignedRole, &t.Status, &t.RetryCount,
		&parentID, &dependsOnJSON, &filesWrittenJSON, &subtaskID,
		&historyJSON, &resultsJSON,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return nil, err
	}

	t.ParentTaskID = parentID.String
	t.SubTaskID = subtaskID.String
	_ = json.Unmarshal([]byte(criteriaJSON), &t.AcceptanceCriteria)
	_ = json.Unmarshal([]byte(historyJSON), &t.ConversationHistory)
	_ = json.Unmarshal([]byte(resultsJSON), &t.ExecutionResults)
	_ = json.Unmarshal([]byte(dependsOnJSON), &t.DependsOn)
	_ = json.Unmarshal([]byte(filesWrittenJSON), &t.FilesWritten)

	t.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	t.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)

	return &t, nil
}
