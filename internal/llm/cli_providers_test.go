package llm

import (
	"encoding/json"
	"testing"
)

func TestScanUsageTokens(t *testing.T) {
	cases := []struct {
		name            string
		in              string
		wantIn, wantOut int
	}{
		{"flat input/output", `{"input_tokens":100,"output_tokens":40}`, 100, 40},
		{"openai spelling", `{"prompt_tokens":12,"completion_tokens":7}`, 12, 7},
		{"gemini camel", `{"promptTokenCount":9,"candidatesTokenCount":3}`, 9, 3},
		{"nested + max wins", `{"a":{"input_tokens":5},"b":{"input_tokens":50,"output_tokens":8}}`, 50, 8},
		{"none", `{"foo":"bar"}`, 0, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			v := mustJSON(t, c.in)
			gi, go_ := scanUsageTokens(v)
			if gi != c.wantIn || go_ != c.wantOut {
				t.Fatalf("got (%d,%d), want (%d,%d)", gi, go_, c.wantIn, c.wantOut)
			}
		})
	}
}

func TestCodexParseJSONL(t *testing.T) {
	t.Run("usage, cumulative max", func(t *testing.T) {
		jsonl := `not json
{"type":"item.completed","item":{"type":"agent_message","text":"hi"}}
{"type":"turn.completed","usage":{"input_tokens":200,"cached_input_tokens":10,"output_tokens":50}}
{"type":"turn.completed","usage":{"input_tokens":260,"cached_input_tokens":10,"output_tokens":75}}`
		in, out, errMsg := codexParseJSONL(jsonl)
		if in != 260 || out != 75 {
			t.Fatalf("tokens: got (%d,%d), want (260,75)", in, out)
		}
		if errMsg != "" {
			t.Fatalf("unexpected errMsg %q", errMsg)
		}
	})
	t.Run("error event captured", func(t *testing.T) {
		jsonl := `{"type":"thread.started","thread_id":"x"}
{"type":"error","message":"401 Unauthorized"}`
		_, _, errMsg := codexParseJSONL(jsonl)
		if errMsg != "401 Unauthorized" {
			t.Fatalf("errMsg: got %q", errMsg)
		}
	})
}

func TestParseGeminiJSON(t *testing.T) {
	t.Run("structured", func(t *testing.T) {
		out := `{"response":"hello world","stats":{"models":{"gemini-2.5-flash":{"tokens":{"promptTokens":11,"candidatesTokens":4,"totalTokens":15}}}}}`
		content, in, o, errMsg := parseGeminiJSON(out)
		if content != "hello world" {
			t.Fatalf("content: %q", content)
		}
		if in != 11 || o != 4 {
			t.Fatalf("tokens: got (%d,%d), want (11,4)", in, o)
		}
		if errMsg != "" {
			t.Fatalf("unexpected errMsg %q", errMsg)
		}
	})
	t.Run("error field", func(t *testing.T) {
		out := `{"session_id":"x","error":{"type":"Error","message":"Please set an Auth method","code":41}}`
		content, _, _, errMsg := parseGeminiJSON(out)
		if errMsg != "Please set an Auth method" {
			t.Fatalf("errMsg: got %q", errMsg)
		}
		if content != "" {
			t.Fatalf("content should be empty on error, got %q", content)
		}
	})
	t.Run("plain text fallback", func(t *testing.T) {
		content, in, o, errMsg := parseGeminiJSON("just text, not json")
		if content != "just text, not json" || in != 0 || o != 0 || errMsg != "" {
			t.Fatalf("got (%q,%d,%d,%q)", content, in, o, errMsg)
		}
	})
}

func TestEnvWithout(t *testing.T) {
	t.Setenv("BUYCOTT_KEEP", "1")
	t.Setenv("BUYCOTT_DROP", "secret")
	env := envWithout("BUYCOTT_DROP")
	var keep, drop bool
	for _, kv := range env {
		if kv == "BUYCOTT_KEEP=1" {
			keep = true
		}
		if kv == "BUYCOTT_DROP=secret" {
			drop = true
		}
	}
	if !keep {
		t.Error("expected BUYCOTT_KEEP to be retained")
	}
	if drop {
		t.Error("expected BUYCOTT_DROP to be removed")
	}
}

func mustJSON(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return v
}
