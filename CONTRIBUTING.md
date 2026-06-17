# Contributing to Buycott

## Contribute a prompt, not code

This repo is built the way it's meant to be used: **outside contributions are prompts, and an AI writes the code.** Instead of opening a PR with a diff, you open one that adds a single `proposals/<slug>.md` describing what you want built. A maintainer reviews the prompt; once it's labeled `approved`, Claude Code runs it and opens the generated change as its own PR (build/vet/test-gated, for a second human review). PRs from non-maintainers that touch code outside `proposals/` are closed automatically.

See **[`proposals/README.md`](proposals/README.md)** for the full flow, the prompt template, and the maintainer/security setup. The rest of this document is the codebase reference that proposal authors (and the agent implementing approved proposals) should follow.

---

The conventions below cover how the codebase is structured, how to get a dev environment running, and the rules an implementation must follow.

---

## Getting started

```bash
git clone <this-repo>
cd buycott

# Build
GOTOOLCHAIN=auto go build ./...

# Vet
GOTOOLCHAIN=auto go vet ./...

# Test
GOTOOLCHAIN=auto go test ./...
```

> **`GOTOOLCHAIN=auto` is required.** `go.mod` specifies Go 1.25 (pulled up by transitive Docker SDK → OpenTelemetry dependencies). Your local toolchain may be older — the flag downloads the right version automatically.

For local runs without Docker Compose, generate a config with the wizard:

```bash
make setup                              # interactive: pick providers/models, handle auth
```

…or copy and edit the example by hand:

```bash
cp config.example.yaml config.yaml
cp .env.example .env   # add at least one API key / subscription token
```

Then:

```bash
make run DIRECTION="Build something small"
```

Prompts are loaded from `prompts/` by default. Set `execution.prompts_dir: ./prompts` in your config or the binary will look in `/etc/buycott/prompts/` (the in-container path).

To work on the subscription CLI providers (`claude-code`, `codex`, `gemini-cli`), install the corresponding CLI and log in — the provider shells out to it. The parser helpers are unit-tested without the CLI, but verifying flags and output shapes requires the real binary.

---

## Package structure and dependency rules

There is a **strict no-circular-imports rule**. Dependencies flow one way only:

```
internal/model      ← shared types only; no imports from other internal packages
internal/config     ← no internal imports
internal/llm        ← no internal imports
internal/executor   ← imports model
internal/state      ← imports model
internal/roles      ← imports model, llm, config
internal/pipeline   ← imports model, state, roles, executor, config
internal/server     ← imports model, config, state, pipeline, roles, llm, executor
cmd/                ← imports config, server
```

`internal/model` exists to break what would otherwise be circular imports between `state` (which persists Tasks) and `pipeline` (which processes Tasks). All shared types — `Task`, `TaskStatus`, `ExecResult`, `Event`, `Release`, `ScanResult` — live in `internal/model/types.go`. If a type needs to be shared across more than one internal package, it belongs there.

Before adding an import, check that it doesn't introduce a cycle. `go build ./...` will catch it, but catching it in design is better.

---

## Adding a new LLM provider

Providers come in two kinds. **API providers** (`anthropic`, `openai`, `gemini`) call a vendor SDK with an API key. **CLI/subscription providers** (`claude-code`, `codex`, `gemini-cli`) shell out to a locally-installed CLI so a role runs on a subscription instead of a metered key. Both implement the same interface:

```go
type Provider interface {
    Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
    Stream(ctx context.Context, req CompletionRequest, out chan<- string) error
    Name() string
}
```

Steps for either kind:

1. Create `internal/llm/{provider}.go` implementing `Provider`.
2. Add a `case "{provider}":` branch to `NewProvider()` in **`internal/llm/factory.go`**.
3. Add `"{provider}"` to the allowed list in `config.validate()` (`internal/config/config.go`).
4. Document it in the README [Supported providers](README.md#supported-providers) table.

**For an API provider** — also add its key field to `config.APIKeysConfig`, return an error from `NewProvider` when the key is empty, add the env var to `.env.example`, and add the model's per-million pricing to `modelPrices` in `internal/state/llmlogs.go` so cost tracking works.

**For a CLI/subscription provider** — model the implementation on `claudecode.go` / `codex.go` / `geminicli.go` and reuse the shared helpers in `cli_common.go` and `claudecode.go`:
- `splitMessages`, `pickTool`, `jsonToolInstruction`, `extractJSON` — fold the message list into the CLI's prompt and emulate forced-tool JSON output (the CLIs have no native tool-forcing).
- `envWithout(...)` — strip the matching API-key env var so the CLI uses its subscription login, not metered billing.
- `scanUsageTokens(...)` — pull token counts out of the CLI's JSON output (tolerant of field-name drift across versions).
- `setupCLIProcess(cmd)` — run in a process group with a bounded `WaitDelay` so context cancellation reliably kills child processes.
- Verify the CLI's actual flags and JSON output shape against the installed binary (`<cli> --help`, a dry run) — the output schemas drift between versions. Surface CLI stderr in returned errors so `RetryingProvider` can detect rate-limit wording.
- These need no API-key config field; auth flows through the CLI's own login. Add the auth flow to `scripts/setup.sh` and the install hint in the `Dockerfile`.

**SDK notes (API providers):**
- Anthropic and OpenAI SDKs (alpha versions) wrap every struct field in `param.Field[T]`. Use `anthropic.F(value)` / `openai.F(value)` — direct struct literal assignment is a compile error.
- Gemini (`google.golang.org/genai`) does not use this pattern.
- For Anthropic responses: check `block.Type == anthropic.ContentBlockTypeText` and read `block.Text` directly — there is no `.AsAny()` method.

---

## Adding a new built-in role

Built-in roles are constructors in `internal/roles/builtin.go` with a matching system prompt file.

1. **Write the prompt** at `prompts/{role_name}.md`. Describe the role's objectives, working environment, response format, and any tool schemas it uses. See existing prompts for examples — they are extensive by design.

2. **Add a constructor** in `internal/roles/builtin.go`. Copy the pattern from `NewBackend` or `NewReviewer` depending on whether it's a worker role or a gating role.

3. **Add a `case "{role_name}":` branch** in `internal/server/local.go`'s `buildRoles()` switch.

4. If the role has a special interface (like `ReviewerRole` or `SecurityReviewerRole`), define the interface in `internal/roles/role.go` and wire the pipeline field in `internal/pipeline/pipeline.go`.

Custom roles defined only in YAML (`system_prompt` or `system_prompt_file`) work automatically without any code changes.

---

## Database schema changes

SQLite is managed by `modernc.org/sqlite` (pure Go — **do not switch to `mattn/go-sqlite3`**, it requires CGO and complicates Docker builds).

Schema changes go in the `migrate()` function in `internal/state/db.go`. Schema versioning uses `PRAGMA user_version`:

- Read the current version at open time
- Apply each version's changes inside a `if currentVersion < N` block
- Bump `PRAGMA user_version` after each version's migrations

**Never modify existing `CREATE TABLE` statements.** Use `ALTER TABLE ADD COLUMN` for additive changes. Existing databases will not be recreated on upgrade — any change must be safe to apply to a running DB.

---

## The `Server` interface and gRPC

`server.Server` (`internal/server/server.go`) is the control surface, implemented twice: `LocalServer` (in-process) and `grpcclient.Client` (remote, behind the `--server` flag). The dashboard and CLI talk to whichever is wired up.

When you **add a method to `server.Server`** you must:

1. Implement it on `LocalServer` (`internal/server/local.go`).
2. Implement it on `grpcclient.Client` (`internal/grpcclient/client.go`) — either as a real RPC or an explicit "not supported over remote connection" error (some control actions only make sense in-process).
3. Update the `mockServer` in `internal/dashboard/server_test.go` so the package still compiles.

If the method must work **remotely**, add an RPC to `proto/buycott.proto`, regenerate, and implement it in both `grpcserver` and `grpcclient`:

```bash
make proto    # needs protoc + protoc-gen-go + protoc-gen-go-grpc on PATH
```

The generated `internal/grpcapi/*.pb.go` files are committed — include them in your PR. Anything reading from the SQLite DB (token stats, conversation logs, …) is only visible to the split Compose/k8s dashboard if it crosses this boundary; a stub that returns `nil` silently blanks the dashboard in remote mode.

---

## Writing tests

Tests use the stdlib `testing` package. Run the full suite with:

```bash
GOTOOLCHAIN=auto go test ./...
```

**Conventions:**

- Test files live alongside the code they test (`foo.go` → `foo_test.go`), same package
- Table-driven tests with `t.Run` for multiple cases
- Use `t.TempDir()` for temporary files — it cleans up automatically
- SQLite tests open a real DB rooted at a `t.TempDir()` (`state.Open(dir)` creates `{dir}/.buycott/state.db`) — do not mock the database layer
- LLM provider calls are tested via a `mockProvider` struct implementing `llm.Provider` — see `internal/roles/security_test.go` for the pattern
- CLI-provider output parsing (token usage, error extraction, JSON-from-prose) is unit-tested on the parser helpers without invoking the real CLI — see `internal/llm/cli_providers_test.go`
- Rate-limit backoff tests use context cancellation to avoid sleeping — see `internal/llm/ratelimit_test.go`

---

## Code conventions

- **No comments explaining what the code does** — well-named identifiers do that. Comments are for non-obvious *why*: a hidden constraint, a workaround for a specific SDK bug, an invariant that would surprise a reader.
- **No placeholder implementations.** If something isn't done, it isn't done — don't add `// TODO: implement` stubs that will rot.
- **Error handling is explicit.** Return errors to callers; don't log-and-continue where returning an error is appropriate.
- **Secure by default.** Parameterized queries only. No `math/rand` for anything security-sensitive. No secrets in logs.
- **Complete file writes only.** The pipeline writes agent output verbatim — this is a good reminder that partial implementations don't work.

---

## Pull request checklist

- [ ] `GOTOOLCHAIN=auto go build ./...` passes
- [ ] `GOTOOLCHAIN=auto go vet ./...` passes  
- [ ] `GOTOOLCHAIN=auto go test ./...` passes
- [ ] New behaviour has tests
- [ ] New built-in roles have a prompt file in `prompts/`; new providers are in the README table
- [ ] Schema changes use versioned migrations, not raw `CREATE TABLE` rewrites
- [ ] `proto/` changes are regenerated (`make proto`) and the generated `internal/grpcapi/` files are committed
- [ ] New `server.Server` methods are implemented on `LocalServer`, `grpcclient.Client`, and the dashboard `mockServer`
- [ ] No new circular imports introduced
- [ ] `MARKETING.md` is not committed (it's gitignored)

---

## Reporting issues

Open a GitHub issue. Include:

- What you were trying to do
- What happened instead
- Relevant config (redact API keys)
- Logs (`buycott logs` or `make compose-logs`)

For security vulnerabilities, please report privately rather than opening a public issue.
