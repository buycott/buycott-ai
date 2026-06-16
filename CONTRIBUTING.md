# Contributing to Buycott

Contributions are welcome — bug fixes, new features, additional LLM providers, new built-in roles, and documentation improvements. This document covers how the codebase is structured, how to get a dev environment running, and what the conventions are.

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

For local runs without Docker Compose, copy and edit the example config:

```bash
cp config.example.yaml config.yaml
cp .env.example .env   # add at least one API key
```

Then:

```bash
make run DIRECTION="Build something small"
```

Prompts are loaded from `prompts/` by default. Set `execution.prompts_dir: ./prompts` in your config or the binary will look in `/etc/buycott/prompts/` (the in-container path).

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

1. Create `internal/llm/{provider}.go` implementing the `Provider` interface:

```go
type Provider interface {
    Complete(ctx context.Context, req CompletionRequest) (CompletionResponse, error)
    Stream(ctx context.Context, req CompletionRequest, out chan<- string) error
    Name() string
}
```

2. Add a `case "{provider}":` branch in `internal/llm/provider.go`'s `NewProvider()` function.

3. Add the provider's API key field to `config.APIKeysConfig` in `internal/config/config.go`.

4. Update `config.validate()` to accept the new provider name.

5. Add the API key env var to `.env.example` and document it in the README providers table.

**SDK notes:**
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

## Writing tests

Tests use the stdlib `testing` package. Run the full suite with:

```bash
GOTOOLCHAIN=auto go test ./...
```

**Conventions:**

- Test files live alongside the code they test (`foo.go` → `foo_test.go`), same package
- Table-driven tests with `t.Run` for multiple cases
- Use `t.TempDir()` for temporary files — it cleans up automatically
- SQLite tests use a real in-memory DB (pass `":memory:"` to `state.Open`) — do not mock the database layer
- LLM provider calls are tested via a `mockProvider` struct implementing `llm.Provider` — see `internal/roles/security_test.go` for the pattern
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
- [ ] New LLM providers or roles have a prompt file in `prompts/`
- [ ] Schema changes use versioned migrations, not raw `CREATE TABLE` rewrites
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
