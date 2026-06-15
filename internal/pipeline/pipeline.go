package pipeline

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"buycott/internal/config"
	"buycott/internal/executor"
	"buycott/internal/model"
	"buycott/internal/roles"
	"buycott/internal/state"
)

type Pipeline struct {
	tasks            *state.TaskStore
	events           *state.EventStore
	releases         *state.ReleaseStore
	registry         *roles.Registry
	pm               roles.PMRole
	reviewer         roles.ReviewerRole
	securityReviewer roles.SecurityReviewerRole
	securityScanCmds []config.ScanCommand
	exec             executor.Executor
	artifacts        string
	direction        string
	maxRetry         int
	releaseInterval  int
	taskTimeout      time.Duration
	minQueueDepth    int
	maxHistoryMsgs   int

	mu         sync.Mutex
	paused     bool
	resumeCh   chan struct{}
	stopCh     chan struct{}
	activeTask *model.Task
}

func New(
	tasks *state.TaskStore,
	events *state.EventStore,
	releases *state.ReleaseStore,
	registry *roles.Registry,
	pm roles.PMRole,
	reviewer roles.ReviewerRole,
	securityReviewer roles.SecurityReviewerRole,
	securityScanCmds []config.ScanCommand,
	exec executor.Executor,
	artifactsPath string,
	direction string,
	maxRetry int,
	releaseInterval int,
	taskTimeout time.Duration,
	minQueueDepth int,
	maxHistoryMsgs int,
) *Pipeline {
	if maxRetry <= 0 {
		maxRetry = 10
	}
	if taskTimeout <= 0 {
		taskTimeout = 5 * time.Minute
	}
	if minQueueDepth <= 0 {
		minQueueDepth = 5
	}
	if maxHistoryMsgs <= 0 {
		maxHistoryMsgs = 20
	}
	return &Pipeline{
		tasks:            tasks,
		events:           events,
		releases:         releases,
		registry:         registry,
		pm:               pm,
		reviewer:         reviewer,
		securityReviewer: securityReviewer,
		securityScanCmds: securityScanCmds,
		exec:             exec,
		artifacts:        artifactsPath,
		direction:        direction,
		maxRetry:         maxRetry,
		releaseInterval:  releaseInterval,
		taskTimeout:      taskTimeout,
		minQueueDepth:    minQueueDepth,
		maxHistoryMsgs:   maxHistoryMsgs,
		resumeCh:         make(chan struct{}, 1),
		stopCh:           make(chan struct{}),
	}
}

func (p *Pipeline) Run(ctx context.Context) error {
	p.emit("pipeline.started", map[string]any{"direction": p.direction})
	for {
		select {
		case <-ctx.Done():
			p.emit("pipeline.stopped", nil)
			return ctx.Err()
		case <-p.stopCh:
			p.emit("pipeline.stopped", nil)
			return nil
		default:
		}

		if p.isPaused() {
			select {
			case <-p.resumeCh:
				p.emit("pipeline.resumed", nil)
			case <-ctx.Done():
				return ctx.Err()
			case <-p.stopCh:
				return nil
			}
		}

		// Proactively top up the queue when it falls below the minimum depth.
		pending, _ := p.tasks.List(model.TaskFilter{Status: model.StatusPending})
		if len(pending) < p.minQueueDepth {
			if err := p.generateTasks(ctx); err != nil {
				log.Printf("proactive generate tasks error: %v", err)
			}
		}

		task, err := p.tasks.FirstPending()
		if err != nil {
			return fmt.Errorf("fetch pending task: %w", err)
		}

		if task == nil {
			// No ready tasks — short sleep before checking again (deps may be in flight).
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
			}
			continue
		}

		if err := p.processTask(ctx, task); err != nil {
			log.Printf("process task %s error: %v", task.ID, err)
		}
	}
}

func (p *Pipeline) Stop() {
	select {
	case <-p.stopCh:
	default:
		close(p.stopCh)
	}
}

func (p *Pipeline) Pause() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.paused = true
	p.emit("pipeline.paused", nil)
}

func (p *Pipeline) Resume() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.paused {
		return
	}
	p.paused = false
	select {
	case p.resumeCh <- struct{}{}:
	default:
	}
}

func (p *Pipeline) isPaused() bool {
	return p.IsPaused()
}

func (p *Pipeline) IsPaused() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.paused
}

func (p *Pipeline) ActiveTask() *model.Task {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.activeTask
}

func (p *Pipeline) setActive(t *model.Task) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.activeTask = t
}

func (p *Pipeline) generateTasks(ctx context.Context) error {
	stats, _ := p.tasks.Stats()

	recentDone, _ := p.tasks.RecentDone(20)
	recentTitles := make([]string, 0, len(recentDone))
	for _, t := range recentDone {
		recentTitles = append(recentTitles, fmt.Sprintf("[%s] %s", t.AssignedRole, t.Title))
	}

	pending, _ := p.tasks.List(model.TaskFilter{Status: model.StatusPending})
	pendingTitles := make([]string, 0, len(pending))
	for _, t := range pending {
		pendingTitles = append(pendingTitles, fmt.Sprintf("[%s] %s", t.AssignedRole, t.Title))
	}

	projectState := map[string]any{
		"stats":              stats,
		"direction":          p.direction,
		"recently_completed": recentTitles,
		"pending_tasks":      pendingTitles,
		"file_tree":          buildFileTree(p.artifacts, 4, 200),
	}
	p.emit("pm.generating_tasks", projectState)

	newTasks, err := p.pm.GenerateTasks(ctx, p.direction, projectState)
	if err != nil {
		return fmt.Errorf("pm generate: %w", err)
	}

	now := time.Now()
	for _, t := range newTasks {
		if t.ID == "" {
			t.ID = uuid.New().String()
		}
		t.Status = model.StatusPending
		t.CreatedAt = now
		t.UpdatedAt = now
		if err := p.tasks.Save(t); err != nil {
			return fmt.Errorf("save task: %w", err)
		}
	}
	p.emit("pm.tasks_generated", map[string]any{"count": len(newTasks)})
	return nil
}

func (p *Pipeline) processTask(ctx context.Context, task *model.Task) error {
	p.setActive(task)
	defer p.setActive(nil)

	task.Status = model.StatusInProgress
	task.UpdatedAt = time.Now()
	if err := p.tasks.Save(task); err != nil {
		return err
	}
	p.emit("task.started", map[string]any{"task_id": task.ID, "role": task.AssignedRole})

	role, ok := p.registry.Get(task.AssignedRole)
	if !ok {
		return p.escalate(task, fmt.Sprintf("unknown role: %s", task.AssignedRole))
	}

	// ── Context trimming ──────────────────────────────────────────────────
	// Keep the first message (initial context) plus the most recent messages
	// so the context window stays bounded across many retry iterations.
	if len(task.ConversationHistory) > p.maxHistoryMsgs {
		first := task.ConversationHistory[0]
		recent := task.ConversationHistory[len(task.ConversationHistory)-(p.maxHistoryMsgs-1):]
		task.ConversationHistory = append([]model.Message{first}, recent...)
	}

	// ── Initial context injection ─────────────────────────────────────────
	// On the very first attempt, pre-populate the conversation history with a
	// rich initial user message that includes project context, file tree, and
	// recent activity. Persisted so retries also see it.
	if len(task.ConversationHistory) == 0 {
		var initMsg strings.Builder
		initMsg.WriteString(fmt.Sprintf("Title: %s\n\nDescription:\n%s\n\nAcceptance Criteria:\n",
			task.Title, task.Description))
		for i, c := range task.AcceptanceCriteria {
			initMsg.WriteString(fmt.Sprintf("%d. %s\n", i+1, c))
		}
		initMsg.WriteString("\n---\n\n")
		initMsg.WriteString(p.buildProjectContext())
		task.ConversationHistory = []model.Message{{Role: "user", Content: initMsg.String()}}
		task.UpdatedAt = time.Now()
		if err := p.tasks.Save(task); err != nil {
			return err
		}
	}

	output, err := role.ProcessTask(ctx, task)
	if err != nil {
		return p.handleFailure(ctx, task, fmt.Sprintf("role error: %v", err), model.ExecResult{ExitCode: -1})
	}
	task.ConversationHistory = append(task.ConversationHistory,
		model.Message{Role: "assistant", Content: output.Narrative},
	)

	// ── Sub-task spawning ─────────────────────────────────────────────────
	// If the engineer requested a sub-task, create it and pause this task.
	if output.SubTask != nil {
		return p.spawnSubTask(task, output.SubTask)
	}

	written, err := roles.WriteFiles(output, p.artifacts)
	if err != nil {
		return p.handleFailure(ctx, task, fmt.Sprintf("write files: %v", err), model.ExecResult{ExitCode: -1})
	}
	task.FilesWritten = append(task.FilesWritten, written...)

	// ── Execution with timeout ────────────────────────────────────────────
	var execResult model.ExecResult
	if len(output.RunCommands) > 0 && output.RunImage != "" {
		execCtx, cancel := context.WithTimeout(ctx, p.taskTimeout)
		execResult, err = p.exec.Run(execCtx, output.RunImage, output.RunCommands, p.artifacts)
		cancel()
		if err != nil {
			return p.handleFailure(ctx, task, fmt.Sprintf("executor error: %v", err), execResult)
		}
		task.ExecutionResults = append(task.ExecutionResults, execResult)
		if execResult.ExitCode != 0 {
			errMsg := fmt.Sprintf("Execution failed (exit %d).\nStdout:\n%s\nStderr:\n%s",
				execResult.ExitCode, execResult.Stdout, execResult.Stderr)
			return p.handleFailure(ctx, task, errMsg, execResult)
		}
	}

	// ── Code review loop ──────────────────────────────────────────────────
	if p.reviewer != nil && task.AssignedRole != "pm" && task.AssignedRole != "reviewer" {
		for {
			p.emit("task.code_review_started", map[string]any{"task_id": task.ID})
			crApproved, crFeedback, crErr := p.reviewer.ReviewCode(ctx, task, output, execResult)
			if crErr != nil {
				log.Printf("reviewer error for task %s: %v — skipping code review", task.ID, crErr)
				break
			}

			if crApproved {
				p.emit("task.code_review_approved", map[string]any{"task_id": task.ID, "feedback": crFeedback})
				break
			}

			p.emit("task.code_review_changes_requested", map[string]any{
				"task_id":  task.ID,
				"feedback": crFeedback,
			})
			task.RetryCount++
			if task.RetryCount >= p.maxRetry {
				return p.escalate(task, "max code review iterations reached.\n\nLast reviewer feedback:\n"+crFeedback)
			}

			task.ConversationHistory = append(task.ConversationHistory,
				model.Message{Role: "user", Content: "Code reviewer requested the following changes. You must implement all of them before resubmitting:\n\n" + crFeedback},
			)
			task.UpdatedAt = time.Now()
			if err := p.tasks.Save(task); err != nil {
				return err
			}

			output, err = role.ProcessTask(ctx, task)
			if err != nil {
				return p.handleFailure(ctx, task, fmt.Sprintf("role error after code review: %v", err), model.ExecResult{ExitCode: -1})
			}
			task.ConversationHistory = append(task.ConversationHistory,
				model.Message{Role: "assistant", Content: output.Narrative},
			)
			written, err = roles.WriteFiles(output, p.artifacts)
			if err != nil {
				return p.handleFailure(ctx, task, fmt.Sprintf("write files after code review: %v", err), model.ExecResult{ExitCode: -1})
			}
			task.FilesWritten = append(task.FilesWritten, written...)

			execResult = model.ExecResult{}
			if len(output.RunCommands) > 0 && output.RunImage != "" {
				execCtx, cancel := context.WithTimeout(ctx, p.taskTimeout)
				execResult, err = p.exec.Run(execCtx, output.RunImage, output.RunCommands, p.artifacts)
				cancel()
				if err != nil {
					return p.handleFailure(ctx, task, fmt.Sprintf("executor error after code review: %v", err), execResult)
				}
				task.ExecutionResults = append(task.ExecutionResults, execResult)
				if execResult.ExitCode != 0 {
					errMsg := fmt.Sprintf("Execution failed after code review changes (exit %d).\nStdout:\n%s\nStderr:\n%s",
						execResult.ExitCode, execResult.Stdout, execResult.Stderr)
					return p.handleFailure(ctx, task, errMsg, execResult)
				}
			}
		}
	}

	// ── Security review ───────────────────────────────────────────────────
	if p.securityReviewer != nil && task.AssignedRole != "pm" && task.AssignedRole != "reviewer" && task.AssignedRole != "security" {
		scanResults, scanErr := p.runSecurityScans(ctx)
		if scanErr != nil {
			log.Printf("security scans for task %s: %v — continuing without scan results", task.ID, scanErr)
		}

		for {
			p.emit("task.security_review_started", map[string]any{"task_id": task.ID})
			srApproved, srFindings, srErr := p.securityReviewer.ReviewSecurity(ctx, task, output, execResult, scanResults)
			if srErr != nil {
				log.Printf("security reviewer error for task %s: %v — skipping security review", task.ID, srErr)
				break
			}

			if srApproved {
				p.emit("task.security_review_approved", map[string]any{"task_id": task.ID})
				break
			}

			p.emit("task.security_review_changes_requested", map[string]any{
				"task_id":  task.ID,
				"findings": srFindings,
			})
			task.RetryCount++
			if task.RetryCount >= p.maxRetry {
				return p.escalate(task, "max security review iterations reached.\n\nLast security findings:\n"+srFindings)
			}

			task.ConversationHistory = append(task.ConversationHistory,
				model.Message{Role: "user", Content: "Security reviewer found issues that must be remediated before this task can proceed:\n\n" + srFindings},
			)
			task.UpdatedAt = time.Now()
			if err := p.tasks.Save(task); err != nil {
				return err
			}

			output, err = role.ProcessTask(ctx, task)
			if err != nil {
				return p.handleFailure(ctx, task, fmt.Sprintf("role error after security review: %v", err), model.ExecResult{ExitCode: -1})
			}
			task.ConversationHistory = append(task.ConversationHistory,
				model.Message{Role: "assistant", Content: output.Narrative},
			)
			written, err = roles.WriteFiles(output, p.artifacts)
			if err != nil {
				return p.handleFailure(ctx, task, fmt.Sprintf("write files after security review: %v", err), model.ExecResult{ExitCode: -1})
			}
			task.FilesWritten = append(task.FilesWritten, written...)

			execResult = model.ExecResult{}
			if len(output.RunCommands) > 0 && output.RunImage != "" {
				execCtx, cancel := context.WithTimeout(ctx, p.taskTimeout)
				execResult, err = p.exec.Run(execCtx, output.RunImage, output.RunCommands, p.artifacts)
				cancel()
				if err != nil {
					return p.handleFailure(ctx, task, fmt.Sprintf("executor error after security review: %v", err), execResult)
				}
				task.ExecutionResults = append(task.ExecutionResults, execResult)
				if execResult.ExitCode != 0 {
					errMsg := fmt.Sprintf("Execution failed after security remediation (exit %d).\nStdout:\n%s\nStderr:\n%s",
						execResult.ExitCode, execResult.Stdout, execResult.Stderr)
					return p.handleFailure(ctx, task, errMsg, execResult)
				}
			}

			// Re-run scans after remediation.
			scanResults, scanErr = p.runSecurityScans(ctx)
			if scanErr != nil {
				log.Printf("security re-scan for task %s: %v", task.ID, scanErr)
			}
		}
	}

	// ── PM tasks: auto-approve, skip PM review ────────────────────────────
	if task.AssignedRole == "pm" {
		task.Status = model.StatusDone
		task.UpdatedAt = time.Now()
		if err := p.tasks.Save(task); err != nil {
			return err
		}
		p.emit("task.done", map[string]any{"task_id": task.ID, "feedback": "PM task auto-approved"})
		p.maybeCheckRelease(ctx)
		p.resumeWaitingParent(ctx, task)
		return nil
	}

	// ── PM review ─────────────────────────────────────────────────────────
	task.Status = model.StatusPendingReview
	task.UpdatedAt = time.Now()
	if err := p.tasks.Save(task); err != nil {
		return err
	}
	p.emit("task.pending_review", map[string]any{"task_id": task.ID})

	approved, feedback, err := p.pm.ReviewTask(ctx, task, execResult)
	if err != nil {
		log.Printf("pm review error for task %s: %v — auto-approving", task.ID, err)
		approved = true
		feedback = "Auto-approved due to review error."
	}

	if approved {
		task.Status = model.StatusDone
		task.UpdatedAt = time.Now()
		if err := p.tasks.Save(task); err != nil {
			return err
		}
		p.emit("task.done", map[string]any{"task_id": task.ID, "feedback": feedback})
		p.maybeCheckRelease(ctx)
		p.resumeWaitingParent(ctx, task)
		return nil
	}

	// PM rejected — reset retry count and re-queue with feedback.
	task.RetryCount = 0
	task.Status = model.StatusPending
	task.UpdatedAt = time.Now()
	task.ConversationHistory = append(task.ConversationHistory,
		model.Message{Role: "user", Content: "PM feedback: " + feedback},
	)
	if err := p.tasks.Save(task); err != nil {
		return err
	}
	p.emit("task.rejected", map[string]any{"task_id": task.ID, "feedback": feedback})
	return nil
}

// spawnSubTask pauses the current task and creates the requested sub-task.
func (p *Pipeline) spawnSubTask(parent *model.Task, req *model.SubTaskRequest) error {
	subtask := &model.Task{
		ID:                 uuid.New().String(),
		Title:              req.Title,
		Description:        req.Description,
		AcceptanceCriteria: req.AcceptanceCriteria,
		AssignedRole:       req.Role,
		Status:             model.StatusPending,
		ParentTaskID:       parent.ID,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}
	if err := p.tasks.Save(subtask); err != nil {
		return fmt.Errorf("save subtask: %w", err)
	}

	parent.SubTaskID = subtask.ID
	parent.Status = model.StatusWaitingSubtask
	parent.UpdatedAt = time.Now()
	if err := p.tasks.Save(parent); err != nil {
		return fmt.Errorf("save waiting parent: %w", err)
	}

	p.emit("task.subtask_spawned", map[string]any{
		"parent_id":  parent.ID,
		"subtask_id": subtask.ID,
		"role":       req.Role,
		"title":      req.Title,
	})
	return nil
}

// resumeWaitingParent checks whether the completed task was a sub-task that
// another task is blocked on, and if so re-queues the parent with the result.
func (p *Pipeline) resumeWaitingParent(ctx context.Context, completed *model.Task) {
	parent, err := p.tasks.FindWaitingForSubtask(completed.ID)
	if err != nil || parent == nil {
		return
	}

	// Inject sub-task result into parent's conversation history.
	summary := fmt.Sprintf(
		"Sub-task completed by %s role.\n\nTitle: %s\n\nOutcome:\n%s",
		completed.AssignedRole, completed.Title,
		lastAssistantMessage(completed.ConversationHistory),
	)
	parent.ConversationHistory = append(parent.ConversationHistory,
		model.Message{Role: "user", Content: "Sub-task result:\n\n" + summary},
	)
	parent.SubTaskID = ""
	parent.Status = model.StatusPending
	parent.UpdatedAt = time.Now()
	if err := p.tasks.Save(parent); err != nil {
		log.Printf("resume waiting parent %s: %v", parent.ID, err)
		return
	}
	p.emit("task.subtask_completed", map[string]any{
		"parent_id":  parent.ID,
		"subtask_id": completed.ID,
	})
}

func lastAssistantMessage(history []model.Message) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Role == "assistant" {
			return history[i].Content
		}
	}
	return "(no output)"
}

func (p *Pipeline) handleFailure(ctx context.Context, task *model.Task, errMsg string, result model.ExecResult) error {
	task.RetryCount++
	p.emit("task.failed", map[string]any{
		"task_id":     task.ID,
		"retry_count": task.RetryCount,
		"error":       errMsg,
	})

	if task.RetryCount >= p.maxRetry {
		return p.escalate(task, errMsg)
	}

	task.ConversationHistory = append(task.ConversationHistory,
		model.Message{Role: "user", Content: fmt.Sprintf("Your previous attempt failed. Error:\n%s\n\nPlease fix the issue and try again.", errMsg)},
	)
	task.Status = model.StatusPending
	task.UpdatedAt = time.Now()
	return p.tasks.Save(task)
}

func (p *Pipeline) escalate(task *model.Task, reason string) error {
	// Roll back files written by this task so they don't pollute the artifacts.
	for _, path := range task.FilesWritten {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			log.Printf("escalate: remove %s: %v", path, err)
		}
	}
	task.FilesWritten = nil

	task.Status = model.StatusEscalated
	task.UpdatedAt = time.Now()
	if err := p.tasks.Save(task); err != nil {
		return err
	}
	p.emit("task.escalated", map[string]any{"task_id": task.ID, "reason": reason})

	escalationTask := &model.Task{
		ID:           uuid.New().String(),
		Title:        fmt.Sprintf("Handle escalation: %s", task.Title),
		Description:  fmt.Sprintf("Task '%s' failed repeatedly.\n\nLast error:\n%s\n\nReview and either fix the task or remove it from the backlog.", task.Title, reason),
		AssignedRole: "pm",
		Status:       model.StatusPending,
		ParentTaskID: task.ID,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	return p.tasks.Save(escalationTask)
}

// runSecurityScans executes all configured scan commands and returns their results.
// A scan command failure is non-fatal; its output and exit code are captured.
func (p *Pipeline) runSecurityScans(ctx context.Context) ([]model.ScanResult, error) {
	if len(p.securityScanCmds) == 0 {
		return nil, nil
	}
	var results []model.ScanResult
	for _, sc := range p.securityScanCmds {
		execCtx, cancel := context.WithTimeout(ctx, p.taskTimeout)
		res, err := p.exec.Run(execCtx, sc.Image, []string{sc.Cmd}, p.artifacts)
		cancel()
		sr := model.ScanResult{
			Tool:     sc.Image,
			Image:    sc.Image,
			Command:  sc.Cmd,
			ExitCode: res.ExitCode,
			Output:   res.Stdout + res.Stderr,
		}
		if err != nil {
			sr.Output += "\nexecutor error: " + err.Error()
		}
		results = append(results, sr)
	}
	return results, nil
}

func (p *Pipeline) emit(eventType string, payload map[string]any) {
	if payload == nil {
		payload = map[string]any{}
	}
	if err := p.events.Append(eventType, payload); err != nil {
		log.Printf("emit event %s: %v", eventType, err)
	}
}

// buildProjectContext returns a rich context string for the current project state.
func (p *Pipeline) buildProjectContext() string {
	var sb strings.Builder

	sb.WriteString("## Project Direction\n")
	sb.WriteString(p.direction)
	sb.WriteString("\n")

	done, _ := p.tasks.RecentDone(15)
	if len(done) > 0 {
		sb.WriteString("\n## Recently Completed Tasks\n")
		for _, t := range done {
			sb.WriteString(fmt.Sprintf("- [%s] %s\n", t.AssignedRole, t.Title))
		}
	}

	pending, _ := p.tasks.List(model.TaskFilter{Status: model.StatusPending})
	if len(pending) > 0 {
		sb.WriteString("\n## Other Pending Tasks\n")
		limit := len(pending)
		if limit > 20 {
			limit = 20
		}
		for _, t := range pending[:limit] {
			sb.WriteString(fmt.Sprintf("- [%s] %s\n", t.AssignedRole, t.Title))
		}
	}

	tree := buildFileTree(p.artifacts, 4, 200)
	if tree != "" {
		sb.WriteString("\n## Current Project Files\n```\n")
		sb.WriteString(tree)
		sb.WriteString("```\n")
	}

	events, _ := p.events.List(0)
	if len(events) > 0 {
		start := len(events) - 20
		if start < 0 {
			start = 0
		}
		sb.WriteString("\n## Recent Activity\n")
		for i := len(events) - 1; i >= start; i-- {
			e := events[i]
			sb.WriteString(fmt.Sprintf("- [%s] %s\n", e.CreatedAt.Format("15:04:05"), e.Type))
		}
	}

	return sb.String()
}

func buildFileTree(root string, maxDepth, maxEntries int) string {
	var sb strings.Builder
	count := 0
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if depth > maxDepth || count >= maxEntries {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".") {
				continue
			}
			if count >= maxEntries {
				sb.WriteString(strings.Repeat("  ", depth) + "...\n")
				return
			}
			rel, _ := filepath.Rel(root, filepath.Join(dir, e.Name()))
			if e.IsDir() {
				sb.WriteString(strings.Repeat("  ", depth) + rel + "/\n")
				count++
				walk(filepath.Join(dir, e.Name()), depth+1)
			} else {
				sb.WriteString(strings.Repeat("  ", depth) + rel + "\n")
				count++
			}
		}
	}
	walk(root, 0)
	return sb.String()
}
