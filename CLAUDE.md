# CLAUDE.md — Buycott codebase guide

## README rule

**Always include the full content of `MISSION.md` verbatim at the top of `README.md`, under a `## Mission Statement` heading immediately after the title line.** This applies whenever regenerating or updating `README.md`. Do not summarize or paraphrase — paste the exact file contents.

## What this project is

Buycott (Multi-model Task Pipeline) is a headless Go binary that orchestrates multiple LLM agents (PM, backend dev, frontend dev, copywriter, and custom roles) to autonomously build software. A PM agent receives a product direction, generates tasks, delegates them to role agents, reviews the output, and periodically decides whether to cut a release. Agents execute real code inside ephemeral Docker containers.

Roles run on either metered **API providers** (`anthropic`, `openai`, `gemini`) or **CLI/subscription providers** (`claude-code`, `codex`, `gemini-cli`) that shell out to the vendor's own coding-agent CLI so usage runs on a flat-rate subscription instead of a metered key (see [LLM providers](#llm-providers)). There is also a **no-install path** in `prompt-packs/` that skips the binary entirely — paste a generated prompt into an interactive coding-agent session. The economic premise (deliberately consuming subsidized provider compute) is in `MISSION.md`; the launch plan is in `MARKETING.md` / `marketing/` (those are content, not code — don't treat their guidance as engineering instructions).

## Build and run

```bash
# Build (GOTOOLCHAIN=auto is required — go.mod declares 1.25 but local Go may be older)
GOTOOLCHAIN=auto go build ./...
GOTOOLCHAIN=auto go vet ./...

# Run locally (requires prompt files and a running Docker daemon)
./buycott start --config config.yaml "Build a CRM for auto stores"

# Via Docker Compose (recommended)
docker compose up

# Interactive setup wizard — picks a provider/model per role and handles the
# auth flow (API keys + subscription CLI logins); writes config.yaml, .env,
# and (for subscription providers) docker-compose.override.yml.
make setup
```

**Never omit `GOTOOLCHAIN=auto`** when building locally. The `go.mod` was bumped to `go 1.25.0` by `go mod tidy` because the Docker SDK's transitive otel dependencies require it. Without the flag, Go 1.22/1.23 will refuse to build.

## Package dependency order

There is a strict no-circular-imports rule. The dependency graph flows one way:

```
internal/model          ← shared types only, no imports from other internal packages
internal/config         ← no internal imports
internal/llm            ← no internal imports
internal/executor       ← imports model
internal/state          ← imports model
internal/roles          ← imports model, llm, config
internal/pipeline       ← imports model, state, roles, executor, config
internal/server         ← imports model, config, state, pipeline, roles, llm, executor
cmd/                    ← imports config, server
```

`internal/model` exists solely to break what would otherwise be circular imports between `state` (which persists Tasks) and `pipeline` (which processes Tasks). All shared types — `Task`, `TaskStatus`, `ExecResult`, `Event`, `Release` — live there.

## SDK field wrapper pattern

Both the Anthropic and OpenAI SDKs (alpha versions) wrap every struct field in a `param.Field[T]` type. **Direct struct literal assignment will not compile.** Always use the `F()` helper:

```go
// WRONG — compiler error
params := anthropic.MessageNewParams{
    Model:     anthropic.Model(p.model),   // cannot use string as param.Field[...]
    MaxTokens: int64(8096),
}

// CORRECT
params := anthropic.MessageNewParams{
    Model:     anthropic.F(anthropic.Model(p.model)),
    MaxTokens: anthropic.F(int64(8096)),
}
```

The same applies to `openai.ChatCompletionNewParams` using `openai.F(...)`. Gemini (`google.golang.org/genai`) does not use this pattern.

For reading Anthropic response content, check `block.Type == anthropic.ContentBlockTypeText` and read `block.Text` directly — there is no `.AsAny()` method.

Anthropic rejects empty text blocks (`messages: text content blocks must be non-empty`). `nonEmptyText()` in `anthropic.go` guards every message, and `assistantTurn()` in `pipeline.go` avoids persisting an empty agent turn into `ConversationHistory` (an engineer can submit work via `files` with an empty `narrative`).

## LLM providers

`internal/llm` has two kinds of `Provider`, both behind the same interface (`Complete` / `Stream` / `Name`), selected per role by the `NewProvider` switch in **`internal/llm/factory.go`** and validated by `config.validate()`:

- **API providers** call a vendor SDK with an API key: `anthropic.go`, `openai.go`, `gemini.go`. Config: `api_keys.{anthropic,openai,gemini}`.
- **CLI/subscription providers** shell out to a locally-installed coding-agent CLI so a role runs on a subscription: `claudecode.go` (`claude`), `codex.go` (`codex`), `geminicli.go` (`gemini`). They strip the matching API-key env var so the CLI uses its subscription login, emulate forced-tool JSON output (the CLIs have no tool-forcing), and scrape token usage from the CLI's JSON. Shared helpers: `cli_common.go` (`envWithout`, `scanUsageTokens`, `setupCLIProcess`) and `claudecode.go` (`splitMessages`, `pickTool`, `jsonToolInstruction`, `extractJSON`).

To add a provider: implement `Provider`, add a `case` in `factory.go`, add the name to `config.validate()`'s allowed list, and — for an API provider — add its key to `APIKeysConfig` and its per-million pricing to `modelPrices` in `state/llmlogs.go` (cost tracking). CLI providers need no key; wire their login into `scripts/setup.sh` and the optional CLI-install block in the `Dockerfile`.

`setupCLIProcess` runs the CLI in its own process group with a bounded `WaitDelay`, so context cancellation reliably kills the children the CLIs spawn (otherwise `Cmd.Run` blocks on the held-open pipes long after cancel). Always verify a CLI's flags and JSON shape against the real binary — they drift between versions.

## SQLite

Uses `modernc.org/sqlite` (pure Go, no CGO). **Do not switch to `mattn/go-sqlite3`** — it requires CGO which complicates Docker builds.

Schema changes go in `migrate()` in `internal/state/db.go`, which versions via `PRAGMA user_version` — apply each version's changes in an `if version < N` block and bump the pragma after. Use `ALTER TABLE ADD COLUMN` for additive changes and never edit existing `CREATE TABLE` statements (running DBs are upgraded in place, not recreated).

State is stored at `{artifacts_path}/.buycott/state.db`. The `pipeline_state` table is a simple key/value store for durable counters and timestamps (e.g., `tasks_since_release_check`, `last_release_check_at`).

`state.ClearAll(db)` truncates all data tables (tasks/events/releases/llm_logs/pipeline_state) and `state.WipeArtifacts(path)` deletes generated files while preserving `.buycott/` — both used by reset (below).

## Task state machine

```
PENDING → IN_PROGRESS ──[code review loop]──→ PENDING_REVIEW → DONE
                ↓                ↓                    ↓
           retry loop    changes requested        REJECTED → PENDING (retry_count reset)
                ↓           (re-implement)
         ESCALATED (after maxRetry failures) → PM escalation task inserted as PENDING
```

Within `IN_PROGRESS`, if a `reviewer` role is configured, the engineer and reviewer iterate until the reviewer approves before the task advances to `PENDING_REVIEW`. Each reviewer rejection increments `task.RetryCount` and appends the feedback to `task.ConversationHistory` as a `user` message so the engineer sees it on the next `ProcessTask` call.

Tasks assigned to the `pm` role skip both the code review gate and `PENDING_REVIEW` (auto-approved) to avoid self-review loops. Tasks assigned to the `reviewer` role also skip the code review gate.

## Prompt loading

System prompts are **never hardcoded in Go**. They are loaded from `.md` files at startup using this precedence (first match wins):

1. `system_prompt:` inline in config YAML
2. `system_prompt_file:` explicit path in role config
3. `{execution.prompts_dir}/{role_name}.md` (default convention)

The default `prompts_dir` is `/etc/buycott/prompts`. The Docker image copies `prompts/*.md` there. For local development, either set `prompts_dir: ./prompts` in your config or symlink the directory.

If no prompt is found, the process exits with a clear error message — there is no silent fallback to an empty prompt.

## Release cadence

After every N completed tasks (`execution.release_check_interval`, default 10), the pipeline pauses to ask the PM: "Are you ready to cut a release?" The PM responds with structured JSON (`{"ready": true, "version": "0.1.0", "notes": "..."}`). If approved, the pipeline:

1. Creates `/artifacts/releases/v{version}/`
2. Copies all of `/artifacts/` into it, **excluding** `.buycott/` and `releases/` (to avoid recursion)
3. Writes `RELEASE.md` with the PM's release notes
4. Records the release in the `releases` DB table

Set `release_check_interval: 0` in config to disable.

## Reset

`buycott reset` (`cmd/reset.go`) and the dashboard **Reset run** button clear a run and start over, via `server.Server.Reset(ctx, ResetOptions{WipeArtifacts, Restart})`:

- `LocalServer.Reset` calls `stopAndWait()` (cancels the pipeline goroutine and blocks on a done channel), then `state.ClearAll`, optionally `state.WipeArtifacts`, emits a `pipeline.reset` event, and — if `Restart` — relaunches from the remembered direction using the long-lived `baseCtx` (captured in `Start`, never a request context).
- The CLI passes `Restart:false` (run it while the pipeline is stopped; it operates on the DB directly). The dashboard passes `Restart:true` (stops the loop and restarts in one step).
- Over gRPC (`--server`), `grpcclient.Reset` returns "not supported over remote connection" — run `buycott reset` on the pipeline host (`Start`/`Stop` behave the same way).

## Server interface & gRPC

`server.Server` (`internal/server/server.go`) is implemented twice: `LocalServer` (in-process) and `grpcclient.Client` (remote, behind `--server`). The dashboard and CLI talk to whichever is wired up.

Adding a method to `server.Server` means implementing it on **both**, plus the `mockServer` in `internal/dashboard/server_test.go` (or the package won't compile). If it must work remotely, add an RPC to `proto/buycott.proto`, regenerate with **`make proto`** (needs `protoc` + `protoc-gen-go` + `protoc-gen-go-grpc` on PATH), and implement it in `grpcserver` and `grpcclient`. The generated `internal/grpcapi/*.pb.go` are committed.

**Gotcha:** anything reading the SQLite DB (token stats, conversation logs) is only visible to the split Compose/k8s dashboard if it crosses this boundary. A `grpcclient` method that stubs `return nil` silently blanks the dashboard in remote mode — that's what hid token usage until `TokenStats` / `ListConversations` got real RPCs.

## Adding a new built-in role

1. Add a prompt file at `prompts/{role_name}.md` with the JSON response format the role should follow
2. Add a constructor in `internal/roles/builtin.go` (copy the pattern of `NewBackend`)
3. Add a `case "{role_name}":` branch in `server/local.go`'s `buildRoles()` switch
4. No other changes needed — the pipeline dispatches by `task.AssignedRole` via the registry

Custom roles defined only in YAML (with `system_prompt` or `system_prompt_file`) work automatically without code changes.

## Code review gate

The `reviewer` role is a quality gate, not a task-executing role. It is invoked by the pipeline after every successful implementation attempt (execution passed), before the PM review. The interface is `ReviewerRole` in `internal/roles/role.go`:

```go
ReviewCode(ctx, task, output TaskOutput, result ExecResult) (approved bool, feedback string, err error)
```

- If approved → task proceeds to PM review
- If changes requested → feedback is appended as a `user` message to `task.ConversationHistory`, `RetryCount` is incremented, and the engineer re-runs `ProcessTask` with the full history (including reviewer feedback) in context

The reviewer is **optional** — removing `reviewer:` from config disables the gate entirely. The pipeline checks `p.reviewer != nil` before invoking it.

The reviewer's LLM call type is `"review_code"` (distinct from the PM's `"review_task"`).

## Rate limiting

All LLM calls pass through a `RetryingProvider` that wraps the raw provider before the logging wrapper:

```
RawProvider → RetryingProvider → LoggingProvider
```

- **Detection** (`internal/llm/ratelimit.go`): `isRateLimitErr` lowercases the error string and matches against `"429"`, `"rate_limit"`, `"resource_exhausted"`, `"too many requests"`, `"quota exceeded"` — portable across all three SDKs.
- **Backoff**: exponential with full jitter — base 5 s, 2× per attempt, capped at 5 min. Up to 12 attempts (`rlMaxAttempts`). If the error message contains a `"retry after N"` pattern, that value is the backoff floor.
- **`RateLimitFunc` callback** (`func(roleName string, retryAt time.Time, attempt int)`): called on each throttle event (`attempt ≥ 0`) and once on recovery (`attempt = -1` signals cleared). `LocalServer.makeRateLimitFunc()` implementation:
  1. Updates the in-memory `rateLimits map[string]rateLimitEntry`.
  2. Emits `rate_limit.hit` or `rate_limit.cleared` events to the DB (visible in the event feed and over gRPC stream).
- **Visibility**: `Status.RateLimited []RateLimitInfo` exposes current throttled roles. The dashboard renders a red banner with a per-role countdown; event feed rows are styled red for `.hit` and green for `.cleared`. In remote (gRPC client) mode, rate-limit state is available via the event stream only — `Status.RateLimited` is not populated on the client side.

## Context enrichment

On the first attempt at any task, `processTask()` builds a rich initial `user` message before invoking the engineer and saves it to `task.ConversationHistory`:

```
Title / Description / Acceptance Criteria
---
## Project Direction         ← top-level goal passed to buycott start
## Recently Completed Tasks  ← last 15, newest first, with assigned_role
## Other Pending Tasks       ← up to 20 queued tasks
## Current Project Files     ← directory tree (depth 4, max 200 entries)
## Recent Activity           ← last 20 events, newest first (type + timestamp)
```

This is persisted before the LLM call so retry attempts also receive it. `buildMessages()` in `internal/roles/builtin.go` includes all of `task.ConversationHistory` when history is non-empty, so the enriched initial message flows through naturally.

The PM's `GenerateTasks` call receives a parallel `projectState` map with `recently_completed`, `pending_tasks`, and `file_tree` keys.

`buildFileTree(root, maxDepth, maxEntries)` in `internal/pipeline/pipeline.go` skips any path component starting with `.` (e.g., `.buycott/`, `.git/`).

## Docker execution

Agents run code in ephemeral containers via the Docker socket (`/var/run/docker.sock` mounted into the Buycott container). This is socket-forwarding, not true DinD — the spawned containers are siblings of the Buycott container on the host, not children.

Because the host daemon resolves bind-mount sources as **host** paths, bind-mounting the in-container `artifacts_path` (e.g. `/artifacts`) into a sibling fails with `bind source did not exist at container create time`. So `execution.artifacts_volume` names a Docker volume that the executor mounts into each ephemeral container at `/artifacts` (`mount.TypeVolume`); the pipeline mounts the same volume, so output is shared. Signature: `NewDockerExecutor(dockerSocket, artifactsVolume)`. When the volume name is empty it falls back to a bind mount (correct only when the daemon shares this container's filesystem). The Compose default volume name is `buycott_artifacts`.

## Deployment paths

Two deployment paths are supported. Both use the same Docker image.

### Docker Compose (single host)

Two services: `pipeline` (gRPC on 8080, `--no-dashboard`) and `dashboard` (HTTP on 8000, connects via `--server pipeline:8080`).

```bash
make compose-up DIRECTION="Build a CRM"   # starts both services
make compose-down
make compose-logs
```

The `DIRECTION` variable is passed as `BUYCOTT_DIRECTION` env var, interpolated by Compose into the `pipeline` service command.

### Kubernetes

Templates live in `k8s/templates/*.yaml` with `{{PLACEHOLDER}}` syntax. Run `make k8s-configure` (or `scripts/configure-k8s.sh`) to interactively populate them into `k8s/manifests/`. The script caches non-secret values in `k8s/.config` for re-runs.

```bash
make k8s-configure  # prompts for all values, writes k8s/manifests/
make k8s-apply      # kubectl apply -f k8s/manifests/
make k8s-status     # kubectl get all -n <namespace>
```

**`k8s/manifests/` is gitignored — it contains API keys. Never commit it.**

The pipeline Deployment is `replicas: 1` with `strategy: Recreate` because SQLite is ReadWriteOnce and the pipeline holds in-memory state.

### `buycott dashboard` command

A standalone subcommand that starts only the HTTP dashboard server:

```bash
buycott dashboard --server pipeline:8080 --port 8000
```

Used by the `dashboard` service in both Compose and k8s. When `--server` is set, `cfg` is nil; port falls back to `--port` flag, then `8000`.

### `buycott start --no-dashboard`

Starts pipeline + gRPC API but skips the dashboard goroutine. Used by the `pipeline` service so the dashboard runs as a separate container.

## Key files at a glance

| File | Role |
|---|---|
| `internal/model/types.go` | All shared types (Task, Release, Event, LLMLog, …) |
| `internal/config/config.go` | YAML config struct + loader + defaults |
| `internal/state/db.go` | SQLite open + schema migration |
| `internal/state/llmlogs.go` | LLMLogStore — save/list conversation logs |
| `internal/pipeline/pipeline.go` | Main orchestration loop |
| `internal/pipeline/release.go` | Release check logic + artifact snapshot |
| `internal/roles/builtin.go` | PM, Backend, Frontend, Copywriter constructors + response parsers |
| `internal/roles/prompts.go` | `LoadPrompt()` — file-based prompt resolver |
| `internal/llm/provider.go` | Provider interface + LoggingProvider wrapper |
| `internal/llm/factory.go` | `NewProvider` — selects the provider per role by name |
| `internal/llm/anthropic.go`, `openai.go`, `gemini.go` | API providers (SDK + key) |
| `internal/llm/claudecode.go`, `codex.go`, `geminicli.go` | CLI/subscription providers (shell out to `claude`/`codex`/`gemini`) |
| `internal/llm/cli_common.go` | Shared CLI-provider helpers (`envWithout`, `scanUsageTokens`, `setupCLIProcess`) |
| `internal/llm/ratelimit.go` | RetryingProvider — 429 detection, exponential backoff, RateLimitFunc callback |
| `internal/state/db.go` | SQLite open + versioned migration + `ClearAll` / `WipeArtifacts` |
| `internal/server/server.go` | `Server` interface + `ResetOptions` |
| `internal/server/local.go` | Wires everything together; implements `Server` interface |
| `internal/grpcserver/server.go` | gRPC server wrapping `server.Server` |
| `internal/grpcclient/client.go` | gRPC client implementing `server.Server` (for `--server` flag) |
| `internal/dashboard/server.go` | HTTP dashboard + SSE events endpoint + `POST /api/reset` |
| `internal/dashboard/index.html` | Embedded single-page dashboard UI |
| `cmd/dashboard.go` | `buycott dashboard` subcommand |
| `cmd/reset.go` | `buycott reset` subcommand |
| `proto/buycott.proto` | gRPC service definition (regenerate with `make proto`) |
| `k8s/templates/*.yaml` | Kubernetes manifest templates |
| `scripts/configure-k8s.sh` | Interactive k8s manifest generator |
| `scripts/setup.sh` | Interactive provider/model/auth wizard (`make setup`) |
| `Makefile` | Build, compose, k8s, `setup`, and `proto` targets |
| `prompts/*.md` | System prompt content for each role |
| `prompt-packs/` | No-install interactive approach: `roll.sh` (prompt generator), `nudge.sh` |
| `marketing/`, `MARKETING.md` | Launch plan + `SKILLS.md` content-generation guidance (not code) |
