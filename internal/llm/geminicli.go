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

// GeminiCLIProvider drives Google's `gemini` CLI in headless mode
// (`gemini --output-format json`) so the pipeline can run on a Google account's
// Gemini access (the CLI's OAuth login / free tier) instead of a metered API
// key. GEMINI_API_KEY / GOOGLE_API_KEY are stripped from the child environment
// so the CLI uses its own login rather than falling back to API billing.
//
// This is distinct from the "gemini" provider, which calls the Gemini API
// directly with a key. As with the other CLI-backed providers, it shells out
// per call and is best for low-volume / experimental use.
type GeminiCLIProvider struct {
	bin   string
	model string
}

func NewGeminiCLIProvider(model string) *GeminiCLIProvider {
	bin := os.Getenv("GEMINI_BIN")
	if bin == "" {
		bin = "gemini"
	}
	return &GeminiCLIProvider{bin: bin, model: model}
}

func (p *GeminiCLIProvider) Name() string {
	m := p.model
	if m == "" {
		m = "default"
	}
	return "gemini-cli/" + m
}

func (p *GeminiCLIProvider) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	system, prompt := splitMessages(req)

	var forcedTool *Tool
	if len(req.Tools) > 0 {
		forcedTool = pickTool(req)
		system = strings.TrimSpace(system + "\n\n" + jsonToolInstruction(forcedTool))
	}
	// The gemini CLI has no separate system channel, so prepend any system
	// instructions to the prompt.
	full := prompt
	if system != "" {
		full = system + "\n\n---\n\n" + prompt
	}

	// `-p ""` forces non-interactive (headless) mode; the full prompt is piped on
	// stdin (the CLI appends -p to stdin, so an empty -p leaves it unchanged).
	// This keeps large prompts off argv, avoiding the per-arg size limit.
	args := []string{"--output-format", "json", "-p", ""}
	if p.model != "" {
		args = append(args, "--model", p.model)
	}

	cmd := exec.CommandContext(ctx, p.bin, args...)
	cmd.Stdin = strings.NewReader(full)
	cmd.Dir = os.TempDir() // neutral cwd: don't pick up this repo's GEMINI.md
	cmd.Env = envWithout("GEMINI_API_KEY", "GOOGLE_API_KEY", "GOOGLE_GENAI_API_KEY")
	setupCLIProcess(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	runErr := cmd.Run()

	// The success payload ({"response","stats"}) is on stdout; failures surface
	// as an {"error":{...}} JSON object that the CLI writes to stdout (exit 0) or
	// stderr (non-zero exit), so check both before trusting the exit code.
	content, inTok, outTok, jsonErr := parseGeminiJSON(stdout.String())
	if jsonErr == "" && content == "" {
		if _, _, _, e := parseGeminiJSON(stderr.String()); e != "" {
			jsonErr = e
		}
	}
	if jsonErr != "" {
		return CompletionResponse{}, fmt.Errorf("gemini-cli: %s", jsonErr)
	}
	if runErr != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		return CompletionResponse{}, fmt.Errorf("gemini-cli: %v: %s", runErr, msg)
	}

	resp := CompletionResponse{Content: content, InputTokens: inTok, OutputTokens: outTok}

	if forcedTool != nil {
		raw, err := extractJSON(content)
		if err != nil {
			return CompletionResponse{}, fmt.Errorf("gemini-cli: tool %q: %w (raw: %.300s)", forcedTool.Name, err, content)
		}
		resp.ToolName = forcedTool.Name
		resp.ToolInput = raw
	}
	return resp, nil
}

func (p *GeminiCLIProvider) Stream(ctx context.Context, req CompletionRequest, ch chan<- string) error {
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

// parseGeminiJSON extracts the response text, token usage, and any error from
// the gemini CLI's `--output-format json` payload, whose schema is
// {"response": string, "stats": object, "error"?: object}. If the output isn't
// the expected JSON (older CLI versions print plain text), the raw output is
// returned as the content with zero usage and no error.
func parseGeminiJSON(out string) (content string, in, outTok int, errMsg string) {
	out = strings.TrimSpace(out)
	var payload struct {
		Response string          `json:"response"`
		Stats    json.RawMessage `json:"stats"`
		Error    *struct {
			Message string `json:"message"`
			Code    any    `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		return out, 0, 0, ""
	}
	if payload.Error != nil && payload.Error.Message != "" {
		return "", 0, 0, payload.Error.Message
	}
	if len(payload.Stats) > 0 {
		var stats any
		if json.Unmarshal(payload.Stats, &stats) == nil {
			in, outTok = scanUsageTokens(stats)
		}
	}
	return strings.TrimSpace(payload.Response), in, outTok, ""
}
