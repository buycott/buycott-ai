package roles

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"buycott/internal/llm"
	"buycott/internal/model"
	"github.com/google/uuid"
)

// ── Tool schemas ─────────────────────────────────────────────────────────────

var submitWorkTool = llm.Tool{
	Name:        "submit_work",
	Description: "Submit your completed implementation for this task.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"narrative": map[string]any{
				"type":        "string",
				"description": "Brief explanation of your approach and key decisions",
			},
			"files": map[string]any{
				"type":        "object",
				"description": "Map of absolute file paths (under /artifacts/) to their full contents",
			},
			"run_image": map[string]any{
				"type":        "string",
				"description": "Docker image to run test commands in (e.g. golang:1.22-alpine)",
			},
			"run_commands": map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Shell commands to verify the implementation",
			},
			"subtask": map[string]any{
				"type":        "object",
				"description": "Optional: spawn a blocking sub-task to another role before continuing",
				"properties": map[string]any{
					"role":        map[string]any{"type": "string"},
					"title":       map[string]any{"type": "string"},
					"description": map[string]any{"type": "string"},
					"acceptance_criteria": map[string]any{
						"type":  "array",
						"items": map[string]any{"type": "string"},
					},
				},
				"required": []any{"role", "title", "description"},
			},
		},
		"required": []any{"narrative", "files"},
	},
}

var submitReviewTool = llm.Tool{
	Name:        "submit_review",
	Description: "Submit your review decision.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"approved": map[string]any{
				"type":        "boolean",
				"description": "true to approve, false to request changes",
			},
			"feedback": map[string]any{
				"type":        "string",
				"description": "Explanation; if rejecting, list specific numbered changes required",
			},
		},
		"required": []any{"approved", "feedback"},
	},
}

var submitTasksTool = llm.Tool{
	Name:        "submit_tasks",
	Description: "Submit the next batch of tasks to execute.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"tasks": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"title":       map[string]any{"type": "string"},
						"description": map[string]any{"type": "string"},
						"acceptance_criteria": map[string]any{
							"type":  "array",
							"items": map[string]any{"type": "string"},
						},
						"assigned_role": map[string]any{"type": "string"},
						"depends_on": map[string]any{
							"type":        "array",
							"items":       map[string]any{"type": "string"},
							"description": "Task IDs that must be done before this task starts",
						},
						"run_image":    map[string]any{"type": "string"},
						"run_commands": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
					},
					"required": []any{"title", "description", "assigned_role"},
				},
			},
		},
		"required": []any{"tasks"},
	},
}

var submitReleaseCheckTool = llm.Tool{
	Name:        "submit_release_check",
	Description: "Submit your release readiness assessment.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"ready":   map[string]any{"type": "boolean"},
			"version": map[string]any{"type": "string", "description": "Semantic version, required when ready=true"},
			"notes":   map[string]any{"type": "string", "description": "Release notes, required when ready=true"},
			"reason":  map[string]any{"type": "string", "description": "Why not ready, required when ready=false"},
		},
		"required": []any{"ready"},
	},
}

// ── baseRole ─────────────────────────────────────────────────────────────────

type baseRole struct {
	name     string
	prompt   string
	provider llm.Provider
}

func (r *baseRole) Name() string         { return r.name }
func (r *baseRole) SystemPrompt() string { return r.prompt }

func (r *baseRole) ProcessTask(ctx context.Context, task *model.Task) (TaskOutput, error) {
	messages := buildMessages(r.prompt, task)
	resp, err := r.provider.Complete(ctx, llm.CompletionRequest{
		Messages:  messages,
		MaxTokens: 8096,
		TaskID:    task.ID,
		CallType:  "process_task",
		Tools:     []llm.Tool{submitWorkTool},
		ToolName:  submitWorkTool.Name,
	})
	if err != nil {
		return TaskOutput{}, fmt.Errorf("%s complete: %w", r.name, err)
	}

	return parseTaskOutput(resp)
}

func buildMessages(systemPrompt string, task *model.Task) []llm.Message {
	msgs := []llm.Message{{Role: "system", Content: systemPrompt}}
	for _, m := range task.ConversationHistory {
		msgs = append(msgs, llm.Message{Role: m.Role, Content: m.Content})
	}
	if len(task.ConversationHistory) == 0 {
		userMsg := fmt.Sprintf("Title: %s\n\nDescription:\n%s\n\nAcceptance Criteria:\n",
			task.Title, task.Description)
		for i, c := range task.AcceptanceCriteria {
			userMsg += fmt.Sprintf("%d. %s\n", i+1, c)
		}
		msgs = append(msgs, llm.Message{Role: "user", Content: userMsg})
	}
	return msgs
}

// ── Task output parsing ───────────────────────────────────────────────────────

type agentResponse struct {
	Narrative   string                `json:"narrative"`
	Files       map[string]string     `json:"files"`
	RunImage    string                `json:"run_image"`
	RunCommands []string              `json:"run_commands"`
	SubTask     *model.SubTaskRequest `json:"subtask,omitempty"`
}

func parseTaskOutput(resp llm.CompletionResponse) (TaskOutput, error) {
	var raw agentResponse

	if len(resp.ToolInput) > 0 {
		// Structured tool-use path — parse directly.
		if err := json.Unmarshal(resp.ToolInput, &raw); err != nil {
			return TaskOutput{}, fmt.Errorf("parse tool input: %w (raw: %.200s)", err, resp.ToolInput)
		}
	} else {
		// Fallback: parse JSON from text content.
		content := stripCodeFence(resp.Content)
		if err := json.Unmarshal([]byte(content), &raw); err != nil {
			return TaskOutput{}, fmt.Errorf("parse agent response: %w (raw: %.200s)", err, content)
		}
	}

	return TaskOutput{
		Narrative:   raw.Narrative,
		Files:       raw.Files,
		RunImage:    raw.RunImage,
		RunCommands: raw.RunCommands,
		SubTask:     raw.SubTask,
	}, nil
}

// WriteFiles writes task output files to the artifacts volume.
// It returns the list of absolute paths that were written.
func WriteFiles(output TaskOutput, artifactsPath string) ([]string, error) {
	var written []string
	for path, content := range output.Files {
		if !strings.HasPrefix(path, "/artifacts/") {
			path = filepath.Join(artifactsPath, filepath.Clean(path))
		} else {
			path = filepath.Join(artifactsPath, strings.TrimPrefix(path, "/artifacts"))
		}
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return written, fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return written, fmt.Errorf("write %s: %w", path, err)
		}
		written = append(written, path)
	}
	return written, nil
}

// ── PM role ───────────────────────────────────────────────────────────────────

type pmRole struct {
	baseRole
}

func (r *pmRole) GenerateTasks(ctx context.Context, direction string, projectState map[string]any) ([]*model.Task, error) {
	stateJSON, _ := json.Marshal(projectState)
	userMsg := fmt.Sprintf(
		"Project direction: %s\n\nCurrent project state:\n%s\n\nGenerate the next batch of tasks.",
		direction, string(stateJSON),
	)

	resp, err := r.provider.Complete(ctx, llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "system", Content: r.prompt},
			{Role: "user", Content: userMsg},
		},
		MaxTokens: 8096,
		CallType:  "generate_tasks",
		Tools:     []llm.Tool{submitTasksTool},
		ToolName:  submitTasksTool.Name,
	})
	if err != nil {
		return nil, fmt.Errorf("pm generate tasks: %w", err)
	}

	return parseTasks(resp)
}

func (r *pmRole) ReviewTask(ctx context.Context, task *model.Task, result model.ExecResult) (bool, string, error) {
	execSummary := fmt.Sprintf("Exit code: %d\nStdout:\n%s\nStderr:\n%s",
		result.ExitCode, result.Stdout, result.Stderr)

	criteriaStr := ""
	for i, c := range task.AcceptanceCriteria {
		criteriaStr += fmt.Sprintf("%d. %s\n", i+1, c)
	}

	const maxMsgLen = 2000
	var historyStr strings.Builder
	for _, m := range task.ConversationHistory {
		body := m.Content
		if len(body) > maxMsgLen {
			body = body[:maxMsgLen] + "\n... (truncated)"
		}
		historyStr.WriteString(fmt.Sprintf("[%s]:\n%s\n\n", m.Role, body))
	}

	userMsg := fmt.Sprintf(
		"Task: %s\n\nDescription:\n%s\n\nAcceptance Criteria:\n%s\n## Implementation History\n%s\n## Execution Result\n%s\n\nReview the completed work.",
		task.Title, task.Description, criteriaStr, historyStr.String(), execSummary,
	)

	resp, err := r.provider.Complete(ctx, llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "system", Content: r.prompt},
			{Role: "user", Content: userMsg},
		},
		MaxTokens: 2048,
		TaskID:    task.ID,
		CallType:  "review_task",
		Tools:     []llm.Tool{submitReviewTool},
		ToolName:  submitReviewTool.Name,
	})
	if err != nil {
		return false, "", fmt.Errorf("pm review: %w", err)
	}

	return parseReview(resp)
}

func (r *pmRole) CheckRelease(ctx context.Context, projectState map[string]any) (bool, string, string, error) {
	stateJSON, _ := json.Marshal(projectState)
	userMsg := fmt.Sprintf(`Assess whether the project is ready to cut a release.

Project state:
%s

Use semantic versioning. If this is the first release, start at 0.1.0.`, string(stateJSON))

	resp, err := r.provider.Complete(ctx, llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "system", Content: r.prompt},
			{Role: "user", Content: userMsg},
		},
		MaxTokens: 2048,
		CallType:  "check_release",
		Tools:     []llm.Tool{submitReleaseCheckTool},
		ToolName:  submitReleaseCheckTool.Name,
	})
	if err != nil {
		return false, "", "", fmt.Errorf("pm check release: %w", err)
	}

	return parseReleaseCheck(resp)
}

// ── Reviewer role ─────────────────────────────────────────────────────────────

type reviewerRole struct {
	baseRole
}

var _ ReviewerRole = (*reviewerRole)(nil)

func NewReviewer(provider llm.Provider, prompt string) ReviewerRole {
	return &reviewerRole{baseRole: baseRole{name: "reviewer", prompt: prompt, provider: provider}}
}

func (r *reviewerRole) ReviewCode(ctx context.Context, task *model.Task, output TaskOutput, result model.ExecResult) (bool, string, error) {
	criteriaStr := ""
	for i, c := range task.AcceptanceCriteria {
		criteriaStr += fmt.Sprintf("%d. %s\n", i+1, c)
	}
	if criteriaStr == "" {
		criteriaStr = "(none specified)"
	}

	const maxFileBytes = 4000
	filesStr := ""
	for path, content := range output.Files {
		if len(content) > maxFileBytes {
			content = content[:maxFileBytes] + "\n... (truncated)"
		}
		filesStr += fmt.Sprintf("\n### %s\n```\n%s\n```\n", path, content)
	}
	if filesStr == "" {
		filesStr = "(no files written)"
	}

	execStr := "No test execution (role produced no run_commands)."
	if len(output.RunCommands) > 0 {
		execStr = fmt.Sprintf("Image: %s\nCommands: %v\nExit code: %d\n\nStdout:\n%s\n\nStderr:\n%s",
			output.RunImage, output.RunCommands, result.ExitCode, result.Stdout, result.Stderr)
	}

	const maxMsgLen = 1500
	var historyStr strings.Builder
	if len(task.ConversationHistory) > 0 {
		historyStr.WriteString("## Prior Review Rounds\n")
		for _, m := range task.ConversationHistory {
			body := m.Content
			if len(body) > maxMsgLen {
				body = body[:maxMsgLen] + "\n... (truncated)"
			}
			historyStr.WriteString(fmt.Sprintf("[%s]:\n%s\n\n", m.Role, body))
		}
	}

	userMsg := fmt.Sprintf(`Review the completed work below.

## Task
**Title:** %s

**Description:**
%s

**Acceptance Criteria:**
%s

%s## Engineer's Narrative (this round)
%s

## Files Written
%s

## Test Execution Results
%s`,
		task.Title, task.Description, criteriaStr, historyStr.String(), output.Narrative, filesStr, execStr)

	resp, err := r.provider.Complete(ctx, llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "system", Content: r.prompt},
			{Role: "user", Content: userMsg},
		},
		MaxTokens: 2048,
		TaskID:    task.ID,
		CallType:  "review_code",
		Tools:     []llm.Tool{submitReviewTool},
		ToolName:  submitReviewTool.Name,
	})
	if err != nil {
		return false, "", fmt.Errorf("reviewer: %w", err)
	}

	return parseReview(resp)
}

// ── Response parsers ──────────────────────────────────────────────────────────

type reviewResponse struct {
	Approved bool   `json:"approved"`
	Feedback string `json:"feedback"`
}

func parseReview(resp llm.CompletionResponse) (bool, string, error) {
	var r reviewResponse
	if len(resp.ToolInput) > 0 {
		if err := json.Unmarshal(resp.ToolInput, &r); err != nil {
			return false, "", fmt.Errorf("parse review tool input: %w", err)
		}
	} else {
		if err := json.Unmarshal([]byte(stripCodeFence(resp.Content)), &r); err != nil {
			return false, "", fmt.Errorf("parse review: %w (raw: %.200s)", err, resp.Content)
		}
	}
	return r.Approved, r.Feedback, nil
}

type releaseCheckResponse struct {
	Ready   bool   `json:"ready"`
	Version string `json:"version"`
	Notes   string `json:"notes"`
	Reason  string `json:"reason"`
}

func parseReleaseCheck(resp llm.CompletionResponse) (bool, string, string, error) {
	var r releaseCheckResponse
	if len(resp.ToolInput) > 0 {
		if err := json.Unmarshal(resp.ToolInput, &r); err != nil {
			return false, "", "", fmt.Errorf("parse release check tool input: %w", err)
		}
	} else {
		if err := json.Unmarshal([]byte(stripCodeFence(resp.Content)), &r); err != nil {
			return false, "", "", fmt.Errorf("parse release check: %w (raw: %.200s)", err, resp.Content)
		}
	}
	return r.Ready, r.Version, r.Notes, nil
}

type tasksEnvelope struct {
	Tasks []taskSpec `json:"tasks"`
}

type taskSpec struct {
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	AssignedRole       string   `json:"assigned_role"`
	DependsOn          []string `json:"depends_on"`
	RunImage           string   `json:"run_image"`
	RunCommands        []string `json:"run_commands"`
}

func parseTasks(resp llm.CompletionResponse) ([]*model.Task, error) {
	var specs []taskSpec

	if len(resp.ToolInput) > 0 {
		var env tasksEnvelope
		if err := json.Unmarshal(resp.ToolInput, &env); err != nil {
			return nil, fmt.Errorf("parse tasks tool input: %w (raw: %.200s)", err, resp.ToolInput)
		}
		specs = env.Tasks
	} else {
		// Fallback: top-level array (old format).
		content := stripCodeFence(resp.Content)
		if err := json.Unmarshal([]byte(content), &specs); err != nil {
			// Try envelope format in text.
			var env tasksEnvelope
			if err2 := json.Unmarshal([]byte(content), &env); err2 != nil {
				return nil, fmt.Errorf("parse tasks: %w (raw: %.200s)", err, content)
			}
			specs = env.Tasks
		}
	}

	tasks := make([]*model.Task, 0, len(specs))
	for _, s := range specs {
		tasks = append(tasks, &model.Task{
			ID:                 uuid.New().String(),
			Title:              s.Title,
			Description:        s.Description,
			AcceptanceCriteria: s.AcceptanceCriteria,
			AssignedRole:       s.AssignedRole,
			DependsOn:          s.DependsOn,
			Status:             model.StatusPending,
		})
	}
	return tasks, nil
}

func stripCodeFence(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.Index(s, "```json"); idx >= 0 {
		s = s[idx+7:]
		if end := strings.Index(s, "```"); end >= 0 {
			s = s[:end]
		}
	} else if strings.HasPrefix(s, "```") {
		s = s[3:]
		if end := strings.Index(s, "```"); end >= 0 {
			s = s[:end]
		}
	}
	return strings.TrimSpace(s)
}

// ── Constructors ──────────────────────────────────────────────────────────────

func NewPM(provider llm.Provider, prompt string) PMRole {
	return &pmRole{baseRole: baseRole{name: "pm", prompt: prompt, provider: provider}}
}

func NewBackend(provider llm.Provider, prompt string) Role {
	return &baseRole{name: "backend", prompt: prompt, provider: provider}
}

func NewFrontend(provider llm.Provider, prompt string) Role {
	return &baseRole{name: "frontend", prompt: prompt, provider: provider}
}

func NewCopywriter(provider llm.Provider, prompt string) Role {
	return &baseRole{name: "copywriter", prompt: prompt, provider: provider}
}

func NewCustomRole(name string, provider llm.Provider, prompt string) Role {
	return &baseRole{name: name, prompt: prompt, provider: provider}
}
