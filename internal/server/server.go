package server

import (
	"context"
	"time"

	"buycott/internal/model"
	"buycott/internal/state"
)

// RateLimitInfo describes a role that is currently waiting out a rate limit.
type RateLimitInfo struct {
	Role    string    `json:"role"`
	HitAt   time.Time `json:"hit_at"`
	RetryAt time.Time `json:"retry_at"`
	Attempt int       `json:"attempt"`
}

type Status struct {
	Running     bool            `json:"running"`
	Paused      bool            `json:"paused"`
	ActiveTask  *model.Task     `json:"active_task,omitempty"`
	QueueLength int             `json:"queue_length"`
	Completed   int             `json:"completed"`
	Escalated   int             `json:"escalated"`
	RateLimited []RateLimitInfo `json:"rate_limited,omitempty"`
}

// ResetOptions controls how a run is torn down and restarted.
type ResetOptions struct {
	// WipeArtifacts also deletes all generated project files under the artifacts
	// directory (the .buycott state dir is preserved).
	WipeArtifacts bool
	// Restart re-launches the pipeline from the original product direction after
	// clearing state, so it regenerates tasks from scratch.
	Restart bool
}

type Server interface {
	Start(ctx context.Context, direction string) error
	Stop() error
	Pause() error
	Resume() error
	// Reset stops the pipeline (if running), clears all run state — tasks,
	// events, releases, LLM logs and counters — and optionally wipes generated
	// artifacts and/or restarts the run from scratch.
	Reset(ctx context.Context, opts ResetOptions) error
	GetStatus() (Status, error)
	GetTask(id string) (*model.Task, error)
	ListTasks(filter model.TaskFilter) ([]*model.Task, error)
	ListEvents(limit int) ([]*model.Event, error)
	StreamEvents(ctx context.Context) (<-chan model.Event, error)
	ListReleases() ([]*model.Release, error)
	// Chat sends message to the named role and streams response tokens on the
	// returned channel (closed when the response is complete).
	// If inject is true, the exchange is appended to the active task's history.
	Chat(ctx context.Context, role, message string, inject bool) (<-chan string, error)
	// ListConversations returns LLM exchange logs, filtered by task and/or role.
	// Pass empty strings to skip a filter. limit=0 means no limit.
	ListConversations(taskID, role string, limit int) ([]*model.LLMLog, error)
	// TokenStats returns per-role aggregated token usage and estimated cost.
	TokenStats() ([]state.RoleTokenStats, error)
}
