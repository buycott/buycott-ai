package roles

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"buycott/internal/llm"
	"buycott/internal/model"
)

var submitSecurityReviewTool = llm.Tool{
	Name:        "submit_security_review",
	Description: "Submit your security review decision.",
	Parameters: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"approved": map[string]any{
				"type":        "boolean",
				"description": "true if no blocking security issues were found, false to request remediation",
			},
			"severity": map[string]any{
				"type":        "string",
				"enum":        []any{"CRITICAL", "HIGH", "MEDIUM", "LOW", "NONE"},
				"description": "Highest severity level found; NONE when approved",
			},
			"findings": map[string]any{
				"type":        "string",
				"description": "Detailed, numbered list of security issues. Empty string when approved.",
			},
		},
		"required": []any{"approved", "severity", "findings"},
	},
}

// ── securityReviewerRole ──────────────────────────────────────────────────────

type securityReviewerRole struct {
	baseRole
}

var _ SecurityReviewerRole = (*securityReviewerRole)(nil)

func NewSecurityReviewer(provider llm.Provider, prompt string) SecurityReviewerRole {
	return &securityReviewerRole{baseRole: baseRole{name: "security", prompt: prompt, provider: provider}}
}

func (r *securityReviewerRole) ReviewSecurity(
	ctx context.Context,
	task *model.Task,
	output TaskOutput,
	execResult model.ExecResult,
	scanResults []model.ScanResult,
) (bool, string, error) {
	userMsg := buildSecurityReviewMessage(task, output, execResult, scanResults)

	resp, err := r.provider.Complete(ctx, llm.CompletionRequest{
		Messages: []llm.Message{
			{Role: "system", Content: r.prompt},
			{Role: "user", Content: userMsg},
		},
		MaxTokens: 4096,
		TaskID:    task.ID,
		CallType:  "review_security",
		Tools:     []llm.Tool{submitSecurityReviewTool},
		ToolName:  submitSecurityReviewTool.Name,
	})
	if err != nil {
		return false, "", fmt.Errorf("security reviewer: %w", err)
	}

	return parseSecurityReview(resp)
}

// ── message builder ───────────────────────────────────────────────────────────

func buildSecurityReviewMessage(task *model.Task, output TaskOutput, execResult model.ExecResult, scanResults []model.ScanResult) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("## Task\n**Title:** %s\n\n**Description:**\n%s\n\n", task.Title, task.Description))

	if len(task.AcceptanceCriteria) > 0 {
		sb.WriteString("**Acceptance Criteria:**\n")
		for i, c := range task.AcceptanceCriteria {
			sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, c))
		}
		sb.WriteString("\n")
	}

	// Files written
	const maxFileBytes = 6000
	sb.WriteString("## Files Written\n")
	if len(output.Files) == 0 {
		sb.WriteString("(no files written)\n")
	} else {
		for path, content := range output.Files {
			if len(content) > maxFileBytes {
				content = content[:maxFileBytes] + "\n... (truncated)"
			}
			sb.WriteString(fmt.Sprintf("\n### %s\n```\n%s\n```\n", path, content))
		}
	}

	// Static analysis results
	sb.WriteString("\n## Static Analysis & CVE Scan Results\n")
	if len(scanResults) == 0 {
		sb.WriteString("(no scan tools were run — review the code directly)\n")
	} else {
		for _, sr := range scanResults {
			exitLabel := "exit 0 (clean)"
			if sr.ExitCode != 0 {
				exitLabel = fmt.Sprintf("exit %d (findings present)", sr.ExitCode)
			}
			sb.WriteString(fmt.Sprintf("\n### %s\n**Image:** `%s`  **Command:** `%s`  **%s**\n\n```\n%s\n```\n",
				sr.Tool, sr.Image, sr.Command, exitLabel, truncate(sr.Output, 4000)))
		}
	}

	// Test execution context
	if len(output.RunCommands) > 0 {
		sb.WriteString(fmt.Sprintf("\n## Test Execution\n**Image:** %s\n**Commands:** %v\n**Exit code:** %d\n\n```\n%s\n```\n",
			output.RunImage, output.RunCommands, execResult.ExitCode,
			truncate(execResult.Stdout+execResult.Stderr, 2000)))
	}

	sb.WriteString("\nPerform a thorough security review of the files above.")
	return sb.String()
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n... (truncated)"
}

// ── response parser ───────────────────────────────────────────────────────────

type securityReviewResponse struct {
	Approved bool   `json:"approved"`
	Severity string `json:"severity"`
	Findings string `json:"findings"`
}

func parseSecurityReview(resp llm.CompletionResponse) (bool, string, error) {
	var r securityReviewResponse
	if len(resp.ToolInput) > 0 {
		if err := json.Unmarshal(resp.ToolInput, &r); err != nil {
			return false, "", fmt.Errorf("parse security review tool input: %w", err)
		}
	} else {
		if err := json.Unmarshal([]byte(stripCodeFence(resp.Content)), &r); err != nil {
			return false, "", fmt.Errorf("parse security review: %w (raw: %.200s)", err, resp.Content)
		}
	}

	findings := r.Findings
	if !r.Approved && r.Severity != "" && r.Severity != "NONE" {
		findings = fmt.Sprintf("[Severity: %s]\n\n%s", r.Severity, findings)
	}
	return r.Approved, findings, nil
}
