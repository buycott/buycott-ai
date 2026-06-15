package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"buycott/internal/config"
	"buycott/internal/model"
)

func TestFireWebhooks_MatchingEventFires(t *testing.T) {
	var (
		mu      sync.Mutex
		received []map[string]any
	)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		json.Unmarshal(body, &payload)
		mu.Lock()
		received = append(received, payload)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
	webhookSrv := httptest.NewServer(handler)
	defer webhookSrv.Close()

	s := &LocalServer{
		cfg: &config.Config{
			Webhooks: []config.WebhookConfig{
				{URL: webhookSrv.URL, Events: []string{"task.done"}},
			},
		},
		rateLimits: make(map[string]rateLimitEntry),
	}

	ev := &model.Event{
		ID:        "e1",
		Type:      "task.done",
		Payload:   map[string]any{"task_id": "t1"},
		CreatedAt: time.Now(),
	}
	s.fireWebhooks(ev)

	// Give the goroutine time to fire.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(received)
		mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(received) != 1 {
		t.Fatalf("expected 1 webhook POST, got %d", len(received))
	}
}

func TestFireWebhooks_NonMatchingEventSkipped(t *testing.T) {
	callCount := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	})
	webhookSrv := httptest.NewServer(handler)
	defer webhookSrv.Close()

	s := &LocalServer{
		cfg: &config.Config{
			Webhooks: []config.WebhookConfig{
				{URL: webhookSrv.URL, Events: []string{"task.done"}},
			},
		},
		rateLimits: make(map[string]rateLimitEntry),
	}

	ev := &model.Event{
		ID:        "e2",
		Type:      "task.started", // not in the webhook's event list
		Payload:   map[string]any{},
		CreatedAt: time.Now(),
	}
	s.fireWebhooks(ev)

	// Give goroutines time to run (none should).
	time.Sleep(100 * time.Millisecond)

	if callCount != 0 {
		t.Errorf("non-matching event should not fire webhook; got %d calls", callCount)
	}
}

func TestFireWebhooks_WildcardMatchesAll(t *testing.T) {
	var (
		mu      sync.Mutex
		count   int
	)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		count++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
	webhookSrv := httptest.NewServer(handler)
	defer webhookSrv.Close()

	s := &LocalServer{
		cfg: &config.Config{
			Webhooks: []config.WebhookConfig{
				{URL: webhookSrv.URL, Events: []string{"*"}},
			},
		},
		rateLimits: make(map[string]rateLimitEntry),
	}

	events := []string{"task.started", "task.done", "pipeline.paused"}
	for _, evType := range events {
		s.fireWebhooks(&model.Event{Type: evType, CreatedAt: time.Now()})
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := count
		mu.Unlock()
		if n >= 3 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if count != 3 {
		t.Errorf("wildcard should match all events; got %d, want 3", count)
	}
}

func TestFireWebhooks_NoWebhooksConfigured(t *testing.T) {
	s := &LocalServer{
		cfg:        &config.Config{},
		rateLimits: make(map[string]rateLimitEntry),
	}
	// Should not panic.
	s.fireWebhooks(&model.Event{Type: "task.done", CreatedAt: time.Now()})
}
