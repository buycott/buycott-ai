package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ClaudeCodeProvider drives the `claude` CLI (Claude Code) in headless mode
// (`-p --output-format json`) instead of calling the Anthropic Messages API
// directly. This lets the pipeline run on a Claude **subscription** (Pro/Max)
// rather than a metered API key: auth is handled entirely by the CLI's own
// OAuth credentials, never via the x-api-key header.
//
// Because it shells out to a process, this provider is best for low-volume /
// experimental use — subscription rate limits will throttle an autonomous
// multi-agent loop far sooner than a pay-as-you-go API key. It exists behind
// the same Provider interface so it can be A/B'd against the api-key providers.
//
// Auth precedence inside the spawned process:
//   - ANTHROPIC_API_KEY is always stripped from the child environment so the CLI
//     uses subscription/OAuth auth rather than silently falling back to API
//     billing.
//   - If oauthToken is set, it is passed as CLAUDE_CODE_OAUTH_TOKEN (the token
//     minted by `claude setup-token`). Otherwise the CLI uses an existing login
//     (keychain / credentials file from `claude login`).
type ClaudeCodeProvider struct {
	bin        string
	model      string
	oauthToken string
}

func NewClaudeCodeProvider(oauthToken, model string) *ClaudeCodeProvider {
	bin := os.Getenv("CLAUDE_CODE_BIN")
	if bin == "" {
		bin = "claude"
	}
	return &ClaudeCodeProvider{bin: bin, model: model, oauthToken: oauthToken}
}

func (p *ClaudeCodeProvider) Name() string {
	m := p.model
	if m == "" {
		m = "default"
	}
	return "claude-code/" + m
}

// ccResult mirrors the JSON emitted by `claude -p --output-format json`.
type ccResult struct {
	Type    string `json:"type"`
	Subtype string `json:"subtype"`
	IsError bool   `json:"is_error"`
	Result  string `json:"result"`
	Usage   struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	} `json:"usage"`
}

func (p *ClaudeCodeProvider) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	system, prompt := splitMessages(req)

	// Forced-tool emulation: the CLI has no direct tool-forcing flag, so when a
	// role wants structured JSON we instruct the model to emit a single JSON
	// object matching the tool's schema, then parse it back into ToolInput.
	var forcedTool *Tool
	if len(req.Tools) > 0 {
		forcedTool = pickTool(req)
		system = strings.TrimSpace(system + "\n\n" + jsonToolInstruction(forcedTool))
	}

	args := []string{"-p", "--output-format", "json"}
	if p.model != "" {
		args = append(args, "--model", p.model)
	}
	if system != "" {
		// --system-prompt replaces the default Claude Code system prompt so the
		// role's persona governs cleanly.
		args = append(args, "--system-prompt", system)
	}
	// We want a pure text/JSON generator, not an autonomous agent editing files:
	// the pipeline runs code in its own Docker containers. Deny Claude Code's
	// built-in tools so it just answers.
	args = append(args, "--disallowed-tools",
		"Bash Edit Write Read Glob Grep WebFetch WebSearch NotebookEdit Task")

	cmd := exec.CommandContext(ctx, p.bin, args...)
	cmd.Stdin = strings.NewReader(prompt)
	// Run in a neutral cwd so CLAUDE.md discovery doesn't pull in this repo.
	cmd.Dir = os.TempDir()
	cmd.Env = p.childEnv()
	setupCLIProcess(cmd)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Surface stderr verbatim so RetryingProvider.isRateLimitErr can match
		// usage-limit / 429 wording and back off.
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		return CompletionResponse{}, fmt.Errorf("claude-code: %v: %s", err, msg)
	}

	var res ccResult
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		return CompletionResponse{}, fmt.Errorf("claude-code: parse output: %w (raw: %.300s)", err, stdout.String())
	}
	if res.IsError {
		return CompletionResponse{}, fmt.Errorf("claude-code: %s", strings.TrimSpace(res.Result))
	}

	resp := CompletionResponse{
		Content:      res.Result,
		InputTokens:  res.Usage.InputTokens + res.Usage.CacheReadInputTokens + res.Usage.CacheCreationInputTokens,
		OutputTokens: res.Usage.OutputTokens,
	}

	if forcedTool != nil {
		raw, err := extractJSON(res.Result)
		if err != nil {
			return CompletionResponse{}, fmt.Errorf("claude-code: tool %q: %w (raw: %.300s)", forcedTool.Name, err, res.Result)
		}
		resp.ToolName = forcedTool.Name
		resp.ToolInput = raw
	}
	return resp, nil
}

// Stream has no native parity here (the CLI's streaming JSON is a different
// shape). The pipeline only streams the interactive chat path, so we satisfy
// the interface by emitting the full completion as a single chunk.
func (p *ClaudeCodeProvider) Stream(ctx context.Context, req CompletionRequest, ch chan<- string) error {
	resp, err := p.Complete(ctx, req)
	if err != nil {
		return err
	}
	select {
	case ch <- resp.Content:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

func (p *ClaudeCodeProvider) childEnv() []string {
	env := os.Environ()[:0:0] // fresh slice, don't alias the package-global
	for _, kv := range os.Environ() {
		// Strip API-key auth so the CLI uses the subscription/OAuth path.
		if strings.HasPrefix(kv, "ANTHROPIC_API_KEY=") {
			continue
		}
		env = append(env, kv)
	}
	if p.oauthToken != "" {
		env = append(env, "CLAUDE_CODE_OAUTH_TOKEN="+p.oauthToken)
	}
	return env
}

// splitMessages flattens the request's message list into the two inputs the CLI
// accepts: a single system prompt and a single prompt string. A lone user
// message is passed through verbatim; multi-turn histories are rendered as a
// labeled transcript.
func splitMessages(req CompletionRequest) (system, prompt string) {
	var systems []string
	var turns []Message
	for _, m := range req.Messages {
		if m.Role == "system" {
			systems = append(systems, m.Content)
		} else {
			turns = append(turns, m)
		}
	}
	system = strings.Join(systems, "\n\n")

	if len(turns) == 1 && turns[0].Role == "user" {
		return system, turns[0].Content
	}
	var b strings.Builder
	for _, m := range turns {
		label := "User"
		if m.Role == "assistant" {
			label = "Assistant"
		}
		fmt.Fprintf(&b, "%s:\n%s\n\n", label, m.Content)
	}
	b.WriteString("Assistant:")
	return system, strings.TrimSpace(b.String())
}

func pickTool(req CompletionRequest) *Tool {
	for i := range req.Tools {
		if req.Tools[i].Name == req.ToolName {
			return &req.Tools[i]
		}
	}
	return &req.Tools[0]
}

func jsonToolInstruction(t *Tool) string {
	schema, _ := json.Marshal(t.Parameters)
	return fmt.Sprintf(
		"You must reply with a single JSON object and nothing else — no prose, no "+
			"explanation, no markdown code fences. The object must conform to this "+
			"JSON Schema for the %q action:\n%s\nDescription: %s",
		t.Name, schema, t.Description)
}

// extractJSON pulls the first balanced JSON object out of a model response,
// tolerating leading prose or ```json fences.
func extractJSON(s string) (json.RawMessage, error) {
	s = strings.TrimSpace(s)
	// Strip a leading ```json / ``` fence and trailing fence if present.
	if strings.HasPrefix(s, "```") {
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[i+1:]
		}
		s = strings.TrimSuffix(strings.TrimSpace(s), "```")
		s = strings.TrimSpace(s)
	}
	start := strings.IndexByte(s, '{')
	if start < 0 {
		return nil, fmt.Errorf("no JSON object found")
	}
	// Walk to the matching closing brace, respecting strings/escapes.
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(s); i++ {
		c := s[i]
		switch {
		case esc:
			esc = false
		case c == '\\' && inStr:
			esc = true
		case c == '"':
			inStr = !inStr
		case inStr:
			// skip
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				candidate := s[start : i+1]
				if !json.Valid([]byte(candidate)) {
					return nil, fmt.Errorf("extracted text is not valid JSON")
				}
				return json.RawMessage(candidate), nil
			}
		}
	}
	return nil, fmt.Errorf("unterminated JSON object")
}
