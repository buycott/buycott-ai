package roles

import (
	"context"
	"encoding/json"
	"testing"

	"buycott/internal/llm"
	"buycott/internal/model"
)

// ── parseSecurityReview ───────────────────────────────────────────────────────

func TestParseSecurityReview_ApprovedViaTool(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"approved": true,
		"severity": "NONE",
		"findings": "",
	})
	approved, findings, err := parseSecurityReview(llm.CompletionResponse{ToolInput: raw})
	if err != nil {
		t.Fatal(err)
	}
	if !approved {
		t.Error("expected approved")
	}
	if findings != "" {
		t.Errorf("expected empty findings, got %q", findings)
	}
}

func TestParseSecurityReview_RejectedWithSeverity(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"approved": false,
		"severity": "CRITICAL",
		"findings": "1. SQL injection in query builder",
	})
	approved, findings, err := parseSecurityReview(llm.CompletionResponse{ToolInput: raw})
	if err != nil {
		t.Fatal(err)
	}
	if approved {
		t.Error("expected not approved")
	}
	if findings == "" {
		t.Error("expected findings to be non-empty")
	}
	// severity label should be prepended
	if findings[:len("[Severity: CRITICAL]")] != "[Severity: CRITICAL]" {
		t.Errorf("expected severity prefix, got %q", findings[:30])
	}
}

func TestParseSecurityReview_TextFallback(t *testing.T) {
	body := `{"approved":true,"severity":"NONE","findings":""}`
	approved, _, err := parseSecurityReview(llm.CompletionResponse{Content: body})
	if err != nil {
		t.Fatal(err)
	}
	if !approved {
		t.Error("expected approved via text fallback")
	}
}

func TestParseSecurityReview_TextFallbackCodeFence(t *testing.T) {
	body := "```json\n{\"approved\":false,\"severity\":\"HIGH\",\"findings\":\"issue\"}\n```"
	approved, findings, err := parseSecurityReview(llm.CompletionResponse{Content: body})
	if err != nil {
		t.Fatal(err)
	}
	if approved {
		t.Error("expected not approved")
	}
	if findings == "" {
		t.Error("expected findings")
	}
}

func TestParseSecurityReview_InvalidJSON(t *testing.T) {
	_, _, err := parseSecurityReview(llm.CompletionResponse{Content: "not json"})
	if err == nil {
		t.Error("expected error on invalid JSON")
	}
}

// ── buildSecurityReviewMessage ────────────────────────────────────────────────

func TestBuildSecurityReviewMessage_NoScans(t *testing.T) {
	task := &model.Task{
		ID:          "t1",
		Title:       "Add login",
		Description: "Implement login endpoint",
	}
	output := TaskOutput{
		Files: map[string]string{"/artifacts/auth.go": "package main"},
	}
	msg := buildSecurityReviewMessage(task, output, model.ExecResult{}, nil)
	if msg == "" {
		t.Fatal("expected non-empty message")
	}
	for _, want := range []string{"Add login", "auth.go", "no scan tools were run"} {
		if !contains(msg, want) {
			t.Errorf("message missing %q", want)
		}
	}
}

func TestBuildSecurityReviewMessage_WithScans(t *testing.T) {
	task := &model.Task{ID: "t2", Title: "T", Description: "D"}
	output := TaskOutput{Files: map[string]string{"/artifacts/app.py": "import os"}}
	scans := []model.ScanResult{
		{Tool: "trivy", Image: "aquasec/trivy:latest", Command: "trivy fs /artifacts", Output: "CVE-2024-1234 CRITICAL", ExitCode: 1},
	}
	msg := buildSecurityReviewMessage(task, output, model.ExecResult{}, scans)
	for _, want := range []string{"trivy", "CVE-2024-1234", "exit 1 (findings present)", "app.py"} {
		if !contains(msg, want) {
			t.Errorf("message missing %q", want)
		}
	}
}

func TestBuildSecurityReviewMessage_WithRunOutput(t *testing.T) {
	task := &model.Task{ID: "t3", Title: "T", Description: "D"}
	output := TaskOutput{
		Files:       map[string]string{"/artifacts/main.go": "package main"},
		RunImage:    "golang:1.22",
		RunCommands: []string{"go test ./..."},
	}
	execResult := model.ExecResult{ExitCode: 0, Stdout: "ok\tpackage\t0.1s"}
	msg := buildSecurityReviewMessage(task, output, execResult, nil)
	if !contains(msg, "Test Execution") {
		t.Error("expected Test Execution section")
	}
}

func TestBuildSecurityReviewMessage_LargeFilesTruncated(t *testing.T) {
	task := &model.Task{ID: "t4", Title: "T", Description: "D"}
	bigContent := string(make([]byte, 8000))
	output := TaskOutput{Files: map[string]string{"/artifacts/big.go": bigContent}}
	msg := buildSecurityReviewMessage(task, output, model.ExecResult{}, nil)
	if !contains(msg, "truncated") {
		t.Error("expected truncation marker for large file")
	}
}

// ── mockProvider ─────────────────────────────────────────────────────────────

type mockProvider struct {
	resp llm.CompletionResponse
	err  error
}

func (m *mockProvider) Complete(_ context.Context, _ llm.CompletionRequest) (llm.CompletionResponse, error) {
	return m.resp, m.err
}

func (m *mockProvider) Stream(_ context.Context, _ llm.CompletionRequest, _ chan<- string) error {
	return m.err
}

func (m *mockProvider) Name() string { return "mock" }

// ── ReviewSecurity integration (mock provider) ────────────────────────────────

func TestReviewSecurity_Approved(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"approved": true, "severity": "NONE", "findings": "",
	})
	prov := &mockProvider{resp: llm.CompletionResponse{ToolInput: raw}}
	r := NewSecurityReviewer(prov, "you are a security reviewer")

	task := &model.Task{ID: "t5", Title: "T", Description: "D"}
	approved, findings, err := r.ReviewSecurity(context.Background(), task, TaskOutput{}, model.ExecResult{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !approved {
		t.Error("expected approved")
	}
	if findings != "" {
		t.Errorf("expected empty findings, got %q", findings)
	}
}

func TestReviewSecurity_Rejected(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"approved": false, "severity": "HIGH", "findings": "1. XSS in template",
	})
	prov := &mockProvider{resp: llm.CompletionResponse{ToolInput: raw}}
	r := NewSecurityReviewer(prov, "you are a security reviewer")

	task := &model.Task{ID: "t6", Title: "T", Description: "D"}
	approved, findings, err := r.ReviewSecurity(context.Background(), task, TaskOutput{}, model.ExecResult{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if approved {
		t.Error("expected rejection")
	}
	if !contains(findings, "XSS") {
		t.Errorf("expected XSS in findings, got %q", findings)
	}
}

func TestReviewSecurity_ProviderError(t *testing.T) {
	prov := &mockProvider{err: context.DeadlineExceeded}
	r := NewSecurityReviewer(prov, "prompt")
	task := &model.Task{ID: "t7", Title: "T", Description: "D"}
	_, _, err := r.ReviewSecurity(context.Background(), task, TaskOutput{}, model.ExecResult{}, nil)
	if err == nil {
		t.Error("expected error from provider")
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
