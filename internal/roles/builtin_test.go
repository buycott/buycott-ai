package roles

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"buycott/internal/llm"
	"buycott/internal/model"
)

// ── helpers ───────────────────────────────────────────────────────────────────

func toolResp(t *testing.T, v any) llm.CompletionResponse {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	return llm.CompletionResponse{ToolInput: b}
}

func textResp(content string) llm.CompletionResponse {
	return llm.CompletionResponse{Content: content}
}

// ── parseTaskOutput ───────────────────────────────────────────────────────────

func TestParseTaskOutput_ToolPath(t *testing.T) {
	resp := toolResp(t, map[string]any{
		"narrative":    "implemented the feature",
		"files":        map[string]any{"/artifacts/main.go": "package main"},
		"run_image":    "golang:1.22-alpine",
		"run_commands": []string{"go test ./..."},
	})

	out, err := parseTaskOutput(resp)
	if err != nil {
		t.Fatalf("parseTaskOutput: %v", err)
	}
	if out.Narrative != "implemented the feature" {
		t.Errorf("Narrative: %q", out.Narrative)
	}
	if out.Files["/artifacts/main.go"] != "package main" {
		t.Errorf("Files: %v", out.Files)
	}
	if out.RunImage != "golang:1.22-alpine" {
		t.Errorf("RunImage: %q", out.RunImage)
	}
	if len(out.RunCommands) != 1 || out.RunCommands[0] != "go test ./..." {
		t.Errorf("RunCommands: %v", out.RunCommands)
	}
}

func TestParseTaskOutput_TextFallback(t *testing.T) {
	content := `{"narrative":"done","files":{"/artifacts/a.txt":"hello"},"run_image":"","run_commands":[]}`
	resp := textResp(content)

	out, err := parseTaskOutput(resp)
	if err != nil {
		t.Fatalf("parseTaskOutput: %v", err)
	}
	if out.Narrative != "done" {
		t.Errorf("Narrative: %q", out.Narrative)
	}
	if out.Files["/artifacts/a.txt"] != "hello" {
		t.Errorf("Files: %v", out.Files)
	}
}

func TestParseTaskOutput_TextFallback_WithCodeFence(t *testing.T) {
	content := "```json\n{\"narrative\":\"ok\",\"files\":{}}\n```"
	resp := textResp(content)

	out, err := parseTaskOutput(resp)
	if err != nil {
		t.Fatalf("parseTaskOutput: %v", err)
	}
	if out.Narrative != "ok" {
		t.Errorf("Narrative: %q", out.Narrative)
	}
}

func TestParseTaskOutput_WithSubtask(t *testing.T) {
	resp := toolResp(t, map[string]any{
		"narrative": "need frontend help",
		"files":     map[string]any{},
		"subtask": map[string]any{
			"role":        "frontend",
			"title":       "Build UI",
			"description": "Create the login page",
		},
	})

	out, err := parseTaskOutput(resp)
	if err != nil {
		t.Fatalf("parseTaskOutput: %v", err)
	}
	if out.SubTask == nil {
		t.Fatal("SubTask should be set")
	}
	if out.SubTask.Role != "frontend" {
		t.Errorf("SubTask.Role: %q", out.SubTask.Role)
	}
	if out.SubTask.Title != "Build UI" {
		t.Errorf("SubTask.Title: %q", out.SubTask.Title)
	}
}

func TestParseTaskOutput_InvalidJSON(t *testing.T) {
	resp := textResp("not json at all")
	_, err := parseTaskOutput(resp)
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// ── parseReview ───────────────────────────────────────────────────────────────

func TestParseReview_ToolPath_Approved(t *testing.T) {
	resp := toolResp(t, map[string]any{"approved": true, "feedback": "LGTM"})
	approved, feedback, err := parseReview(resp)
	if err != nil {
		t.Fatalf("parseReview: %v", err)
	}
	if !approved {
		t.Error("expected approved=true")
	}
	if feedback != "LGTM" {
		t.Errorf("feedback: %q", feedback)
	}
}

func TestParseReview_ToolPath_Rejected(t *testing.T) {
	resp := toolResp(t, map[string]any{"approved": false, "feedback": "missing tests"})
	approved, feedback, err := parseReview(resp)
	if err != nil {
		t.Fatalf("parseReview: %v", err)
	}
	if approved {
		t.Error("expected approved=false")
	}
	if feedback != "missing tests" {
		t.Errorf("feedback: %q", feedback)
	}
}

func TestParseReview_TextFallback(t *testing.T) {
	resp := textResp(`{"approved":true,"feedback":"all good"}`)
	approved, _, err := parseReview(resp)
	if err != nil {
		t.Fatalf("parseReview: %v", err)
	}
	if !approved {
		t.Error("expected approved=true")
	}
}

// ── parseReleaseCheck ─────────────────────────────────────────────────────────

func TestParseReleaseCheck_ReadyTool(t *testing.T) {
	resp := toolResp(t, map[string]any{
		"ready":   true,
		"version": "1.0.0",
		"notes":   "First release",
	})
	ready, version, notes, err := parseReleaseCheck(resp)
	if err != nil {
		t.Fatalf("parseReleaseCheck: %v", err)
	}
	if !ready {
		t.Error("expected ready=true")
	}
	if version != "1.0.0" {
		t.Errorf("version: %q", version)
	}
	if notes != "First release" {
		t.Errorf("notes: %q", notes)
	}
}

func TestParseReleaseCheck_NotReady(t *testing.T) {
	resp := toolResp(t, map[string]any{
		"ready":  false,
		"reason": "tests not passing",
	})
	ready, _, _, err := parseReleaseCheck(resp)
	if err != nil {
		t.Fatalf("parseReleaseCheck: %v", err)
	}
	if ready {
		t.Error("expected ready=false")
	}
}

func TestParseReleaseCheck_TextFallback(t *testing.T) {
	resp := textResp(`{"ready":true,"version":"0.1.0","notes":"beta"}`)
	ready, version, _, err := parseReleaseCheck(resp)
	if err != nil {
		t.Fatalf("parseReleaseCheck: %v", err)
	}
	if !ready || version != "0.1.0" {
		t.Errorf("ready=%v version=%q", ready, version)
	}
}

// ── parseTasks ────────────────────────────────────────────────────────────────

func TestParseTasks_ToolEnvelope(t *testing.T) {
	resp := toolResp(t, map[string]any{
		"tasks": []any{
			map[string]any{
				"title":        "Add login",
				"description":  "Create login endpoint",
				"assigned_role": "backend",
			},
			map[string]any{
				"title":        "Add tests",
				"description":  "Write unit tests",
				"assigned_role": "backend",
				"depends_on":   []any{"will-be-replaced"},
			},
		},
	})

	tasks, err := parseTasks(resp)
	if err != nil {
		t.Fatalf("parseTasks: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
	if tasks[0].Title != "Add login" {
		t.Errorf("task 0 title: %q", tasks[0].Title)
	}
	if tasks[0].Status != model.StatusPending {
		t.Errorf("task 0 status: %q", tasks[0].Status)
	}
	if tasks[0].ID == "" {
		t.Error("task ID should be set")
	}
}

func TestParseTasks_TextArrayFallback(t *testing.T) {
	content := `[{"title":"T1","description":"d","assigned_role":"frontend"},{"title":"T2","description":"d","assigned_role":"frontend"}]`
	resp := textResp(content)

	tasks, err := parseTasks(resp)
	if err != nil {
		t.Fatalf("parseTasks text array: %v", err)
	}
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
}

func TestParseTasks_TextEnvelopeFallback(t *testing.T) {
	content := `{"tasks":[{"title":"T1","description":"d","assigned_role":"pm"}]}`
	resp := textResp(content)

	tasks, err := parseTasks(resp)
	if err != nil {
		t.Fatalf("parseTasks text envelope: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
}

func TestParseTasks_DependsOnPropagated(t *testing.T) {
	resp := toolResp(t, map[string]any{
		"tasks": []any{
			map[string]any{
				"title":        "Task A",
				"description":  "d",
				"assigned_role": "backend",
				"depends_on":   []any{"id-abc"},
			},
		},
	})

	tasks, err := parseTasks(resp)
	if err != nil {
		t.Fatalf("parseTasks: %v", err)
	}
	if len(tasks[0].DependsOn) != 1 || tasks[0].DependsOn[0] != "id-abc" {
		t.Errorf("DependsOn: %v", tasks[0].DependsOn)
	}
}

// ── stripCodeFence ────────────────────────────────────────────────────────────

func TestStripCodeFence(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{`{"key":"val"}`, `{"key":"val"}`},
		{"```json\n{\"key\":\"val\"}\n```", `{"key":"val"}`},
		{"```\n{\"key\":\"val\"}\n```", `{"key":"val"}`},
		{"  ```json\n{}\n```  ", `{}`},
		{"", ""},
	}
	for _, tc := range cases {
		got := stripCodeFence(tc.input)
		if got != tc.want {
			t.Errorf("stripCodeFence(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

// ── WriteFiles ────────────────────────────────────────────────────────────────

func TestWriteFiles_WritesUnderArtifacts(t *testing.T) {
	dir := t.TempDir()

	out := TaskOutput{
		Files: map[string]string{
			"/artifacts/src/main.go": "package main\n",
			"/artifacts/README.md":   "# project\n",
		},
	}

	written, err := WriteFiles(out, dir)
	if err != nil {
		t.Fatalf("WriteFiles: %v", err)
	}
	if len(written) != 2 {
		t.Fatalf("written count: %d", len(written))
	}

	for _, path := range written {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("file not found: %s: %v", path, err)
		}
	}
}

func TestWriteFiles_RelativePathPrefixedWithArtifacts(t *testing.T) {
	dir := t.TempDir()

	out := TaskOutput{
		Files: map[string]string{
			"src/app.js": "console.log('hello')",
		},
	}

	written, err := WriteFiles(out, dir)
	if err != nil {
		t.Fatalf("WriteFiles: %v", err)
	}
	if len(written) != 1 {
		t.Fatalf("written count: %d", len(written))
	}

	// File should land under dir.
	if !filepath.IsAbs(written[0]) {
		t.Errorf("written path should be absolute: %q", written[0])
	}
	content, _ := os.ReadFile(written[0])
	if string(content) != "console.log('hello')" {
		t.Errorf("file content: %q", string(content))
	}
}

func TestWriteFiles_EmptyFiles(t *testing.T) {
	dir := t.TempDir()
	out := TaskOutput{Files: map[string]string{}}
	written, err := WriteFiles(out, dir)
	if err != nil {
		t.Fatalf("WriteFiles: %v", err)
	}
	if len(written) != 0 {
		t.Errorf("expected 0 written, got %d", len(written))
	}
}

func TestWriteFiles_CreatesNestedDirs(t *testing.T) {
	dir := t.TempDir()
	out := TaskOutput{
		Files: map[string]string{
			"/artifacts/a/b/c/deep.txt": "content",
		},
	}
	_, err := WriteFiles(out, dir)
	if err != nil {
		t.Fatalf("WriteFiles: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "a/b/c/deep.txt"))
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(data) != "content" {
		t.Errorf("content: %q", string(data))
	}
}
