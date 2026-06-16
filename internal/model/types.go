package model

import "time"

type TaskStatus string

const (
	StatusPending        TaskStatus = "pending"
	StatusInProgress     TaskStatus = "in_progress"
	StatusPendingReview  TaskStatus = "pending_review"
	StatusDone           TaskStatus = "done"
	StatusRejected       TaskStatus = "rejected"
	StatusEscalated      TaskStatus = "escalated"
	StatusWaitingSubtask TaskStatus = "waiting_subtask"
)

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ExecResult struct {
	Stdout   string        `json:"stdout"`
	Stderr   string        `json:"stderr"`
	ExitCode int           `json:"exit_code"`
	Duration time.Duration `json:"duration_ns"`
}

type Task struct {
	ID                  string       `json:"id"`
	Title               string       `json:"title"`
	Description         string       `json:"description"`
	AcceptanceCriteria  []string     `json:"acceptance_criteria"`
	AssignedRole        string       `json:"assigned_role"`
	Status              TaskStatus   `json:"status"`
	RetryCount          int          `json:"retry_count"`
	ParentTaskID        string       `json:"parent_task_id,omitempty"`
	DependsOn           []string     `json:"depends_on,omitempty"`
	FilesWritten        []string     `json:"files_written,omitempty"`
	SubTaskID           string       `json:"subtask_id,omitempty"`
	ConversationHistory []Message    `json:"conversation_history"`
	ExecutionResults    []ExecResult `json:"execution_results"`
	CreatedAt           time.Time    `json:"created_at"`
	UpdatedAt           time.Time    `json:"updated_at"`
}

// SubTaskRequest is included in TaskOutput when an agent needs to block on
// another role completing a piece of work before it can continue.
type SubTaskRequest struct {
	Role               string   `json:"role"`
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	AcceptanceCriteria []string `json:"acceptance_criteria,omitempty"`
}

type TaskFilter struct {
	Status TaskStatus
}

type Event struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Payload   map[string]any `json:"payload"`
	CreatedAt time.Time      `json:"created_at"`
}

type Release struct {
	ID        string    `json:"id"`
	Version   string    `json:"version"`
	Notes     string    `json:"notes"`
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"created_at"`
}

// ScanResult holds the output of one static-analysis / CVE-scan tool run.
type ScanResult struct {
	Tool     string `json:"tool"`
	Image    string `json:"image"`
	Command  string `json:"command"`
	Output   string `json:"output"`
	ExitCode int    `json:"exit_code"`
}

// LLMLog records every prompt/response exchange with any model.
type LLMLog struct {
	ID           string    `json:"id"`
	TaskID       string    `json:"task_id,omitempty"`
	Role         string    `json:"role"`
	Model        string    `json:"model"`
	CallType     string    `json:"call_type"`
	Messages     []Message `json:"messages"`
	Response     string    `json:"response"`
	InputTokens  int       `json:"input_tokens"`
	OutputTokens int       `json:"output_tokens"`
	DurationMs   int64     `json:"duration_ms"`
	CreatedAt    time.Time `json:"created_at"`
}
