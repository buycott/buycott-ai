package llm

import "testing"

func TestExtractJSON(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string // expected extracted JSON, "" means expect error
	}{
		{"plain", `{"ready":true,"version":"0.1.0"}`, `{"ready":true,"version":"0.1.0"}`},
		{"fenced", "```json\n{\"a\":1}\n```", `{"a":1}`},
		{"leading prose", "Here you go:\n{\"a\":1}", `{"a":1}`},
		{"nested + trailing", `{"a":{"b":2}} trailing junk`, `{"a":{"b":2}}`},
		{"brace in string", `{"note":"a } b","ok":true}`, `{"note":"a } b","ok":true}`},
		{"escaped quote", `{"s":"x \" } y"}`, `{"s":"x \" } y"}`},
		{"none", `no json here`, ""},
		{"unterminated", `{"a":1`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := extractJSON(c.in)
			if c.want == "" {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if string(got) != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestSplitMessages(t *testing.T) {
	t.Run("single user passthrough", func(t *testing.T) {
		sys, prompt := splitMessages(CompletionRequest{Messages: []Message{
			{Role: "system", Content: "be terse"},
			{Role: "user", Content: "hello"},
		}})
		if sys != "be terse" {
			t.Fatalf("system: got %q", sys)
		}
		if prompt != "hello" {
			t.Fatalf("prompt: got %q", prompt)
		}
	})

	t.Run("multi-turn transcript", func(t *testing.T) {
		_, prompt := splitMessages(CompletionRequest{Messages: []Message{
			{Role: "user", Content: "first"},
			{Role: "assistant", Content: "ok"},
			{Role: "user", Content: "second"},
		}})
		want := "User:\nfirst\n\nAssistant:\nok\n\nUser:\nsecond\n\nAssistant:"
		if prompt != want {
			t.Fatalf("got %q\nwant %q", prompt, want)
		}
	})

	t.Run("multiple system messages joined", func(t *testing.T) {
		sys, _ := splitMessages(CompletionRequest{Messages: []Message{
			{Role: "system", Content: "a"},
			{Role: "system", Content: "b"},
			{Role: "user", Content: "x"},
		}})
		if sys != "a\n\nb" {
			t.Fatalf("system: got %q", sys)
		}
	})
}
