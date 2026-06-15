package llm

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

type Message struct {
	Role    string // "system" | "user" | "assistant"
	Content string
}

// Tool describes a callable function the LLM may invoke.
// Parameters is a JSON Schema object (map[string]any) describing the input.
type Tool struct {
	Name        string
	Description string
	Parameters  map[string]any
}

type CompletionRequest struct {
	Messages    []Message
	MaxTokens   int
	Temperature float64
	// If Tools is set, the provider switches to tool-use mode and the response
	// will have ToolName/ToolInput populated instead of (or in addition to) Content.
	Tools    []Tool
	ToolName string // force this specific tool; ignored if Tools is empty
	// Metadata for conversation logging — ignored by providers.
	TaskID   string
	CallType string
}

type CompletionResponse struct {
	Content      string
	ToolName     string          // set when the model used a tool
	ToolInput    json.RawMessage // raw JSON arguments from the tool call
	InputTokens  int
	OutputTokens int
}

type Provider interface {
	Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
	// Stream sends text tokens to ch as they arrive. Tool-use responses are not
	// streamed; callers that need tool output should use Complete instead.
	Stream(ctx context.Context, req CompletionRequest, ch chan<- string) error
	Name() string
}

// LogFunc is called after every successful LLM exchange.
type LogFunc func(roleName, modelName string, req CompletionRequest, response string, inputTokens, outputTokens int, durationMs int64)

// RateLimitFunc is called when a rate-limit error is encountered or cleared.
// attempt=-1 signals the limit has cleared and the role should be unmarked.
type RateLimitFunc func(roleName string, retryAt time.Time, attempt int)

// LoggingProvider wraps any Provider and calls logFn after each exchange.
type LoggingProvider struct {
	inner    Provider
	roleName string
	logFn    LogFunc
}

func NewLoggingProvider(inner Provider, roleName string, fn LogFunc) Provider {
	if fn == nil {
		return inner
	}
	return &LoggingProvider{inner: inner, roleName: roleName, logFn: fn}
}

func (p *LoggingProvider) Name() string { return p.inner.Name() }

func (p *LoggingProvider) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	start := time.Now()
	resp, err := p.inner.Complete(ctx, req)
	if err == nil {
		// Log both text content and tool input as the response string.
		logged := resp.Content
		if resp.ToolInput != nil {
			logged = string(resp.ToolInput)
		}
		p.logFn(p.roleName, p.inner.Name(), req, logged,
			resp.InputTokens, resp.OutputTokens, time.Since(start).Milliseconds())
	}
	return resp, err
}

func (p *LoggingProvider) Stream(ctx context.Context, req CompletionRequest, ch chan<- string) error {
	start := time.Now()
	buf := make(chan string, 256)
	var full strings.Builder

	fwdDone := make(chan struct{})
	go func() {
		defer close(fwdDone)
		for chunk := range buf {
			full.WriteString(chunk)
			select {
			case ch <- chunk:
			case <-ctx.Done():
				for range buf {
				}
				return
			}
		}
	}()

	err := p.inner.Stream(ctx, req, buf)
	close(buf)
	<-fwdDone

	if err == nil && ctx.Err() == nil {
		p.logFn(p.roleName, p.inner.Name(), req, full.String(), 0, 0, time.Since(start).Milliseconds())
	}
	return err
}
