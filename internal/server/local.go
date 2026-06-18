package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"buycott/internal/config"
	"buycott/internal/executor"
	"buycott/internal/llm"
	"buycott/internal/model"
	"buycott/internal/pipeline"
	"buycott/internal/roles"
	"buycott/internal/state"
	"github.com/google/uuid"
)

type rateLimitEntry struct {
	hitAt   time.Time
	retryAt time.Time
	attempt int
}

type LocalServer struct {
	cfg     *config.Config
	db      *sql.DB
	mu      sync.Mutex
	pipe    *pipeline.Pipeline
	running bool
	cancel  context.CancelFunc
	done    chan struct{} // closed when the running pipeline goroutine exits
	// remembered across restarts (e.g. Reset) — set on the first Start.
	direction string
	baseCtx   context.Context
	tasks     *state.TaskStore
	events    *state.EventStore
	releases  *state.ReleaseStore
	llmLogs   *state.LLMLogStore
	// populated by Start, used by Chat
	registry  *roles.Registry
	providers map[string]llm.Provider
	// rate-limit state (keyed by role name)
	rateLimits   map[string]rateLimitEntry
	rateLimitsMu sync.RWMutex
}

func NewLocal(cfg *config.Config) (*LocalServer, error) {
	db, err := state.Open(cfg.Project.ArtifactsPath)
	if err != nil {
		return nil, fmt.Errorf("open state db: %w", err)
	}
	return &LocalServer{
		cfg:        cfg,
		db:         db,
		tasks:      state.NewTaskStore(db),
		events:     state.NewEventStore(db),
		releases:   state.NewReleaseStore(db),
		llmLogs:    state.NewLLMLogStore(db),
		rateLimits: make(map[string]rateLimitEntry),
	}, nil
}

func (s *LocalServer) Start(ctx context.Context, direction string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("pipeline already running")
	}

	// Remember how to re-launch this run (used by Reset --restart). The base
	// context is the long-lived one from the caller, never a request context.
	s.direction = direction
	s.baseCtx = ctx

	registry, pm, reviewer, securityReviewer, securityScanCmds, providers, err := s.buildRoles()
	if err != nil {
		return fmt.Errorf("build roles: %w", err)
	}
	s.registry = registry
	s.providers = providers

	exec, err := executor.NewDockerExecutor(s.cfg.Execution.DockerSocket, s.cfg.Execution.ArtifactsVolume)
	if err != nil {
		return fmt.Errorf("docker executor: %w", err)
	}

	s.pipe = pipeline.New(
		s.tasks, s.events, s.releases, registry, pm, reviewer, securityReviewer, securityScanCmds, exec,
		s.cfg.Project.ArtifactsPath,
		direction,
		s.cfg.Execution.MaxRetries,
		s.cfg.Execution.ReleaseCheckInterval,
		s.cfg.Execution.TaskTimeout,
		s.cfg.Execution.MinQueueDepth,
		s.cfg.Execution.MaxHistoryMessages,
	)

	pipeCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.running = true
	done := make(chan struct{})
	s.done = done

	s.startWebhookNotifier(pipeCtx)

	go func() {
		defer func() {
			s.mu.Lock()
			s.running = false
			s.mu.Unlock()
			close(done)
		}()
		_ = s.pipe.Run(pipeCtx)
	}()

	return nil
}

// stopAndWait cancels the running pipeline (if any) and blocks until its
// goroutine has fully exited, so callers can safely mutate shared state.
func (s *LocalServer) stopAndWait() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.cancel()
	done := s.done
	s.mu.Unlock()
	<-done
}

// Reset tears down the current run, clears all persisted state, and optionally
// wipes generated artifacts and/or restarts the pipeline from scratch.
func (s *LocalServer) Reset(ctx context.Context, opts ResetOptions) error {
	s.stopAndWait()

	if err := state.ClearAll(s.db); err != nil {
		return fmt.Errorf("clear state: %w", err)
	}

	// Drop any lingering rate-limit markers from the old run.
	s.rateLimitsMu.Lock()
	s.rateLimits = make(map[string]rateLimitEntry)
	s.rateLimitsMu.Unlock()

	if opts.WipeArtifacts {
		if err := state.WipeArtifacts(s.cfg.Project.ArtifactsPath); err != nil {
			return fmt.Errorf("wipe artifacts: %w", err)
		}
	}

	// First event of the fresh run.
	_ = s.events.Append("pipeline.reset", map[string]any{
		"wiped_artifacts": opts.WipeArtifacts,
		"restarted":       opts.Restart,
	})

	if opts.Restart {
		base := s.baseCtx
		if base == nil {
			base = ctx
		}
		if err := s.Start(base, s.direction); err != nil {
			return fmt.Errorf("restart after reset: %w", err)
		}
	}
	return nil
}

func (s *LocalServer) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return fmt.Errorf("pipeline not running")
	}
	s.cancel()
	return nil
}

func (s *LocalServer) Pause() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pipe == nil {
		return fmt.Errorf("pipeline not started")
	}
	s.pipe.Pause()
	return nil
}

func (s *LocalServer) Resume() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pipe == nil {
		return fmt.Errorf("pipeline not started")
	}
	s.pipe.Resume()
	return nil
}

func (s *LocalServer) GetStatus() (Status, error) {
	s.mu.Lock()
	running := s.running
	var active *model.Task
	var paused bool
	if s.pipe != nil {
		active = s.pipe.ActiveTask()
		paused = s.pipe.IsPaused()
	}
	s.mu.Unlock()

	stats, err := s.tasks.Stats()
	if err != nil {
		return Status{}, err
	}

	s.rateLimitsMu.RLock()
	var rl []RateLimitInfo
	for role, entry := range s.rateLimits {
		rl = append(rl, RateLimitInfo{
			Role:    role,
			HitAt:   entry.hitAt,
			RetryAt: entry.retryAt,
			Attempt: entry.attempt,
		})
	}
	s.rateLimitsMu.RUnlock()

	pending := stats[model.StatusPending] + stats[model.StatusInProgress] + stats[model.StatusPendingReview]
	return Status{
		Running:     running,
		Paused:      paused,
		ActiveTask:  active,
		QueueLength: pending,
		Completed:   stats[model.StatusDone],
		Escalated:   stats[model.StatusEscalated],
		RateLimited: rl,
	}, nil
}

func (s *LocalServer) GetTask(id string) (*model.Task, error) {
	return s.tasks.Get(id)
}

func (s *LocalServer) ListTasks(filter model.TaskFilter) ([]*model.Task, error) {
	return s.tasks.List(filter)
}

func (s *LocalServer) ListEvents(limit int) ([]*model.Event, error) {
	return s.events.List(limit)
}

func (s *LocalServer) ListReleases() ([]*model.Release, error) {
	return s.releases.List()
}

func (s *LocalServer) StreamEvents(ctx context.Context) (<-chan model.Event, error) {
	ch := make(chan model.Event, 64)
	go func() {
		defer close(ch)
		var cursor time.Time

		existing, err := s.events.List(0)
		if err == nil {
			for _, e := range existing {
				select {
				case ch <- *e:
				case <-ctx.Done():
					return
				}
				cursor = e.CreatedAt
			}
		}

		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				newEvents, err := s.events.Since(cursor)
				if err != nil {
					continue
				}
				for _, e := range newEvents {
					select {
					case ch <- *e:
					case <-ctx.Done():
						return
					}
					cursor = e.CreatedAt
				}
			}
		}
	}()
	return ch, nil
}

func (s *LocalServer) makeRateLimitFunc() llm.RateLimitFunc {
	return func(roleName string, retryAt time.Time, attempt int) {
		s.rateLimitsMu.Lock()
		if attempt < 0 {
			delete(s.rateLimits, roleName)
			s.rateLimitsMu.Unlock()
			_ = s.events.Append("rate_limit.cleared", map[string]any{"role": roleName})
		} else {
			s.rateLimits[roleName] = rateLimitEntry{hitAt: time.Now(), retryAt: retryAt, attempt: attempt}
			s.rateLimitsMu.Unlock()
			_ = s.events.Append("rate_limit.hit", map[string]any{
				"role":          roleName,
				"retry_at_unix": retryAt.Unix(),
				"attempt":       attempt,
			})
		}
	}
}

func (s *LocalServer) buildRoles() (*roles.Registry, roles.PMRole, roles.ReviewerRole, roles.SecurityReviewerRole, []config.ScanCommand, map[string]llm.Provider, error) {
	registry := roles.NewRegistry()
	providers := make(map[string]llm.Provider)

	logSink := s.makeLLMLogFunc()
	rlFn := s.makeRateLimitFunc()

	var pm roles.PMRole
	var reviewer roles.ReviewerRole
	var securityReviewer roles.SecurityReviewerRole
	var securityScanCmds []config.ScanCommand

	for name, roleCfg := range s.cfg.Roles {
		rawProvider, err := llm.NewProvider(roleCfg, s.cfg.APIKeys)
		if err != nil {
			return nil, nil, nil, nil, nil, nil, fmt.Errorf("role %s: %w", name, err)
		}
		// Provider chain: raw → retrying (handles 429s) → logging (records exchanges)
		retryingProvider := llm.NewRetryingProvider(rawProvider, name, rlFn, llm.WithMaxWait(s.cfg.Execution.RateLimitMaxWait))
		provider := llm.NewLoggingProvider(retryingProvider, name, logSink)
		providers[name] = provider

		prompt, err := roles.LoadPrompt(name, roleCfg, s.cfg.Execution.PromptsDir)
		if err != nil {
			return nil, nil, nil, nil, nil, nil, err
		}

		switch name {
		case "pm":
			pm = roles.NewPM(provider, prompt)
			registry.Register(pm)
		case "reviewer":
			reviewer = roles.NewReviewer(provider, prompt)
			registry.Register(reviewer)
		case "security":
			securityReviewer = roles.NewSecurityReviewer(provider, prompt)
			securityScanCmds = roleCfg.ScanCommands
			registry.Register(securityReviewer)
		case "backend":
			registry.Register(roles.NewBackend(provider, prompt))
		case "frontend":
			registry.Register(roles.NewFrontend(provider, prompt))
		case "copywriter":
			registry.Register(roles.NewCopywriter(provider, prompt))
		default:
			registry.Register(roles.NewCustomRole(name, provider, prompt))
		}
	}

	if pm == nil {
		return nil, nil, nil, nil, nil, nil, fmt.Errorf("pm role is required in config")
	}
	// reviewer and securityReviewer are optional — nil disables the respective gate
	return registry, pm, reviewer, securityReviewer, securityScanCmds, providers, nil
}

func (s *LocalServer) makeLLMLogFunc() llm.LogFunc {
	return func(roleName, modelName string, req llm.CompletionRequest, response string, inputTokens, outputTokens int, durationMs int64) {
		msgs := make([]model.Message, len(req.Messages))
		for i, m := range req.Messages {
			msgs[i] = model.Message{Role: m.Role, Content: m.Content}
		}
		entry := &model.LLMLog{
			ID:           uuid.New().String(),
			TaskID:       req.TaskID,
			Role:         roleName,
			Model:        modelName,
			CallType:     req.CallType,
			Messages:     msgs,
			Response:     response,
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
			DurationMs:   durationMs,
			CreatedAt:    time.Now().UTC(),
		}
		_ = s.llmLogs.Save(entry)
	}
}

func (s *LocalServer) ListConversations(taskID, role string, limit int) ([]*model.LLMLog, error) {
	return s.llmLogs.List(taskID, role, limit)
}

func (s *LocalServer) TokenStats() ([]state.RoleTokenStats, error) {
	return s.llmLogs.TokenStats()
}

// startWebhookNotifier watches the event store and fires configured webhooks.
func (s *LocalServer) startWebhookNotifier(ctx context.Context) {
	if len(s.cfg.Webhooks) == 0 {
		return
	}
	go func() {
		var cursor time.Time
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				newEvents, err := s.events.Since(cursor)
				if err != nil {
					continue
				}
				for _, ev := range newEvents {
					cursor = ev.CreatedAt
					s.fireWebhooks(ev)
				}
			}
		}
	}()
}

func (s *LocalServer) fireWebhooks(ev *model.Event) {
	payload, _ := json.Marshal(ev)
	for _, wh := range s.cfg.Webhooks {
		matched := false
		for _, pattern := range wh.Events {
			if pattern == "*" || pattern == ev.Type {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		url := wh.URL
		go func() {
			req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(payload))
			if err != nil {
				return
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				log.Printf("webhook %s: %v", url, err)
				return
			}
			resp.Body.Close()
		}()
	}
}

func (s *LocalServer) Chat(ctx context.Context, role, message string, inject bool) (<-chan string, error) {
	s.mu.Lock()
	provider, hasProvider := s.providers[role]
	r, hasRole := s.registry.Get(role)
	var activeTask *model.Task
	if s.pipe != nil {
		activeTask = s.pipe.ActiveTask()
	}
	s.mu.Unlock()

	if !hasProvider || !hasRole {
		return nil, fmt.Errorf("unknown role: %q", role)
	}

	var chatTaskID string
	if activeTask != nil {
		chatTaskID = activeTask.ID
	}
	req := llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "system", Content: r.SystemPrompt()},
			{Role: "user", Content: message},
		},
		MaxTokens: 4096,
		TaskID:    chatTaskID,
		CallType:  "chat",
	}

	out := make(chan string, 64)
	go func() {
		defer close(out)

		pipe := make(chan string, 64)
		var full strings.Builder

		// Stream from LLM, tee into full for optional injection.
		go func() {
			defer close(pipe)
			if err := provider.Stream(ctx, req, pipe); err != nil && ctx.Err() == nil {
				select {
				case out <- fmt.Sprintf("\n[stream error: %v]", err):
				default:
				}
			}
		}()

		for chunk := range pipe {
			full.WriteString(chunk)
			select {
			case out <- chunk:
			case <-ctx.Done():
				return
			}
		}

		if inject && activeTask != nil {
			activeTask.ConversationHistory = append(activeTask.ConversationHistory,
				model.Message{Role: "user", Content: message},
				model.Message{Role: "assistant", Content: full.String()},
			)
			activeTask.UpdatedAt = time.Now()
			_ = s.tasks.Save(activeTask)
		}
	}()

	return out, nil
}
