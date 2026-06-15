package llm

import (
	"context"
	"testing"
)

// ── LoggingProvider tests ─────────────────────────────────────────────────────

func TestLoggingProvider_NilLogFn_ReturnsInner(t *testing.T) {
	inner := &stubProvider{name: "test", response: CompletionResponse{Content: "hi"}}
	p := NewLoggingProvider(inner, "role", nil)
	// Should be the same object (unwrapped).
	if p != inner {
		t.Error("nil log fn should return inner provider directly")
	}
}

func TestLoggingProvider_Complete_LogsOnSuccess(t *testing.T) {
	inner := &stubProvider{name: "model-x", response: CompletionResponse{
		Content:      "result",
		InputTokens:  10,
		OutputTokens: 5,
	}}

	var loggedRole, loggedModel, loggedResponse string
	var loggedInput, loggedOutput int

	logFn := func(role, model string, req CompletionRequest, response string, in, out int, ms int64) {
		loggedRole = role
		loggedModel = model
		loggedResponse = response
		loggedInput = in
		loggedOutput = out
	}

	p := NewLoggingProvider(inner, "backend", logFn)
	resp, err := p.Complete(context.Background(), CompletionRequest{MaxTokens: 100})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Content != "result" {
		t.Errorf("content: %q", resp.Content)
	}
	if loggedRole != "backend" {
		t.Errorf("logged role: %q", loggedRole)
	}
	if loggedModel != "model-x" {
		t.Errorf("logged model: %q", loggedModel)
	}
	if loggedResponse != "result" {
		t.Errorf("logged response: %q", loggedResponse)
	}
	if loggedInput != 10 {
		t.Errorf("logged input tokens: %d", loggedInput)
	}
	if loggedOutput != 5 {
		t.Errorf("logged output tokens: %d", loggedOutput)
	}
}

func TestLoggingProvider_Complete_LogsToolInput(t *testing.T) {
	inner := &stubProvider{name: "model-x", response: CompletionResponse{
		ToolName:  "submit_work",
		ToolInput: []byte(`{"narrative":"done"}`),
	}}

	var loggedResponse string
	logFn := func(role, model string, req CompletionRequest, response string, in, out int, ms int64) {
		loggedResponse = response
	}

	p := NewLoggingProvider(inner, "backend", logFn)
	p.Complete(context.Background(), CompletionRequest{})

	if loggedResponse != `{"narrative":"done"}` {
		t.Errorf("logged tool input: %q", loggedResponse)
	}
}

func TestLoggingProvider_Complete_DoesNotLogOnError(t *testing.T) {
	inner := &stubProvider{name: "m", failN: 1, failErr: context.DeadlineExceeded}

	called := false
	logFn := func(role, model string, req CompletionRequest, response string, in, out int, ms int64) {
		called = true
	}

	p := NewLoggingProvider(inner, "role", logFn)
	p.Complete(context.Background(), CompletionRequest{})

	if called {
		t.Error("log fn should not be called on error")
	}
}

func TestLoggingProvider_Stream_LogsOnSuccess(t *testing.T) {
	inner := &stubProvider{name: "m", response: CompletionResponse{}}

	var loggedContent string
	logFn := func(role, model string, req CompletionRequest, response string, in, out int, ms int64) {
		loggedContent = response
	}

	p := NewLoggingProvider(inner, "role", logFn)
	ch := make(chan string, 10)
	if err := p.Stream(context.Background(), CompletionRequest{}, ch); err != nil {
		t.Fatalf("Stream: %v", err)
	}
	close(ch) // The stub writes "ok" and returns; ch is already closed by the forwarder.
	// Wait for the forwarding goroutine to complete.
	// LoggingProvider closes buf and waits for fwdDone.
	// After Stream returns, logFn should have been called.
	if loggedContent != "ok" {
		t.Errorf("stream log content: %q, want ok", loggedContent)
	}
}
