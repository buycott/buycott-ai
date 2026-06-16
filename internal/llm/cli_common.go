package llm

import (
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// setupCLIProcess configures a CLI subprocess so that cancelling its context
// reliably tears it down. The CLIs (codex, gemini) spawn child processes that
// would otherwise survive a kill of just the parent and keep the output pipes
// open — making Cmd.Run block long after the context is done. Running the
// command in its own process group and killing the whole group on cancel, plus
// a bounded WaitDelay, keeps cancellation prompt.
func setupCLIProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			// Negative pid => signal the whole process group.
			return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	cmd.WaitDelay = 5 * time.Second
}

// envWithout returns a copy of the current environment with any variable whose
// name matches one of the given names removed. CLI-backed providers use this to
// strip API-key env vars so the underlying CLI authenticates via the user's
// subscription/OAuth login rather than silently falling back to metered API
// billing.
func envWithout(names ...string) []string {
	drop := make(map[string]bool, len(names))
	for _, n := range names {
		drop[n] = true
	}
	src := os.Environ()
	out := make([]string, 0, len(src))
	for _, kv := range src {
		name := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			name = kv[:i]
		}
		if drop[name] {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// scanUsageTokens walks a decoded JSON value (from json.Unmarshal into `any`)
// and returns the largest input and output token counts it can find, matching
// the several key spellings used by different CLI/SDK output formats. Token
// counters are cumulative over a run, so the maximum observed value is the
// total. Returns (0,0) when nothing matches — callers treat that as "unknown".
func scanUsageTokens(v any) (in, out int) {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if n, ok := asJSONInt(val); ok {
				switch strings.ToLower(strings.ReplaceAll(k, "_", "")) {
				case "inputtokens", "prompttokens", "prompttokencount", "input", "prompt":
					if n > in {
						in = n
					}
				case "outputtokens", "completiontokens", "candidatestokens", "candidatestokencount", "output", "candidates":
					if n > out {
						out = n
					}
				}
			}
			ci, co := scanUsageTokens(val)
			if ci > in {
				in = ci
			}
			if co > out {
				out = co
			}
		}
	case []any:
		for _, e := range t {
			ci, co := scanUsageTokens(e)
			if ci > in {
				in = ci
			}
			if co > out {
				out = co
			}
		}
	}
	return in, out
}

func asJSONInt(v any) (int, bool) {
	if f, ok := v.(float64); ok {
		return int(f), true
	}
	return 0, false
}
