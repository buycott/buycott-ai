package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CodexProvider drives the OpenAI `codex` CLI in headless mode
// (`codex exec --json`) so the pipeline can run on a ChatGPT subscription
// instead of a metered OpenAI API key. Auth is handled by the CLI's own login
// (`codex login`, stored under ~/.codex); OPENAI_API_KEY is stripped from the
// child environment so it can't silently fall back to API billing.
//
// Like the other CLI-backed providers this shells out per call, so it's best
// for low-volume / experimental use — subscription limits throttle an
// autonomous multi-agent loop quickly. The pipeline runs generated code in its
// own Docker containers, so Codex is used purely as a text/JSON generator: it
// runs read-only and outside any git repo.
type CodexProvider struct {
	bin   string
	model string
}

func NewCodexProvider(model string) *CodexProvider {
	bin := os.Getenv("CODEX_BIN")
	if bin == "" {
		bin = "codex"
	}
	return &CodexProvider{bin: bin, model: model}
}

func (p *CodexProvider) Name() string {
	m := p.model
	if m == "" {
		m = "default"
	}
	return "codex/" + m
}

func (p *CodexProvider) Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error) {
	system, prompt := splitMessages(req)

	var forcedTool *Tool
	if len(req.Tools) > 0 {
		forcedTool = pickTool(req)
		system = strings.TrimSpace(system + "\n\n" + jsonToolInstruction(forcedTool))
	}
	// Codex exec takes a single prompt with no separate system channel, so fold
	// any system instructions in ahead of the conversation.
	full := prompt
	if system != "" {
		full = system + "\n\n---\n\n" + prompt
	}

	// Neutral working dir + a file to capture only the final assistant message.
	work, err := os.MkdirTemp("", "codex-")
	if err != nil {
		return CompletionResponse{}, fmt.Errorf("codex: tempdir: %w", err)
	}
	defer os.RemoveAll(work)
	lastMsg := filepath.Join(work, "last.txt")

	args := []string{
		"exec",
		"--json",
		"--skip-git-repo-check",
		"--sandbox", "read-only",
		"--cd", work,
		"--output-last-message", lastMsg,
	}
	if p.model != "" {
		args = append(args, "--model", p.model)
	}
	args = append(args, "-") // read the prompt from stdin

	cmd := exec.CommandContext(ctx, p.bin, args...)
	cmd.Stdin = strings.NewReader(full)
	cmd.Env = envWithout("OPENAI_API_KEY")
	setupCLIProcess(cmd)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		return CompletionResponse{}, fmt.Errorf("codex: %v: %s", err, msg)
	}

	content := ""
	if b, err := os.ReadFile(lastMsg); err == nil {
		content = strings.TrimSpace(string(b))
	}

	inTok, outTok, errMsg := codexParseJSONL(stdout.String())
	// codex exits 0 even when the turn ends in an error event, and writes no
	// final message — surface the error instead of returning empty content.
	if content == "" {
		if errMsg != "" {
			return CompletionResponse{}, fmt.Errorf("codex: %s", errMsg)
		}
		return CompletionResponse{}, fmt.Errorf("codex: no output produced")
	}
	resp := CompletionResponse{Content: content, InputTokens: inTok, OutputTokens: outTok}

	if forcedTool != nil {
		raw, err := extractJSON(content)
		if err != nil {
			return CompletionResponse{}, fmt.Errorf("codex: tool %q: %w (raw: %.300s)", forcedTool.Name, err, content)
		}
		resp.ToolName = forcedTool.Name
		resp.ToolInput = raw
	}
	return resp, nil
}

func (p *CodexProvider) Stream(ctx context.Context, req CompletionRequest, ch chan<- string) error {
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

// codexParseJSONL scans codex's JSONL event stream (`codex exec --json`) for
// cumulative token usage and the last error message. The schema is verified
// against codex 0.140: a `turn.completed`/usage event carries input_tokens and
// output_tokens, and failures surface as {"type":"error","message":"..."}.
// The token finder is tolerant of schema drift across codex versions.
func codexParseJSONL(out string) (in, outTok int, errMsg string) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] != '{' {
			continue
		}
		var v map[string]any
		if json.Unmarshal([]byte(line), &v) != nil {
			continue
		}
		if t, _ := v["type"].(string); t == "error" {
			if m, ok := v["message"].(string); ok && m != "" {
				errMsg = m
			}
		}
		ci, co := scanUsageTokens(v)
		if ci > in {
			in = ci
		}
		if co > outTok {
			outTok = co
		}
	}
	return in, outTok, errMsg
}
