<p align="center">
  <img src="images/buycott-banner.png" alt="Buycott — Buy it. Hurt them." width="100%">
</p>

<p align="center">
  <strong>Every token costs them money. Every task burns their runway.<br>Ship real software. Do it automatically. Watch the money disappear.</strong>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.25-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Go 1.25">
  <img src="https://img.shields.io/badge/Docker-ready-2496ED?style=flat-square&logo=docker&logoColor=white" alt="Docker">
  <img src="https://img.shields.io/badge/providers-API_keys_+_subscriptions-FF4500?style=flat-square" alt="LLM Providers">
  <img src="https://img.shields.io/badge/human_intervention-none-black?style=flat-square" alt="Autonomous">
</p>

---

## Table of Contents

- [Mission Statement](#mission-statement)
- [What is Buycott?](#what-is-buycott)
- [How it works](#how-it-works)
- [Quick start](#quick-start)
  - [Prerequisites](#prerequisites)
  - [1. Clone and configure](#1-clone-and-configure)
  - [2. Run](#2-run)
  - [3. Watch it work](#3-watch-it-work)
  - [4. Dashboard](#4-dashboard)
- [Deployment](#deployment)
  - [Docker Compose](#docker-compose--recommended-for-single-host)
  - [Kubernetes](#kubernetes)
- [Configuration](#configuration)
  - [Supported providers](#supported-providers)
- [Role system](#role-system)
  - [System prompts](#system-prompts)
  - [Engineer output format](#engineer-output-format)
  - [Adding a custom role](#adding-a-custom-role)
- [CLI reference](#cli-reference)
  - [Pipeline control](#pipeline-control)
  - [Inspection](#inspection)
  - [Conversation logs](#conversation-logs)
  - [Interactive chat](#interactive-chat)
  - [Dashboard (standalone)](#dashboard-standalone)
  - [Remote mode](#remote-mode)
- [Releases](#releases)
- [Task lifecycle](#task-lifecycle)
- [Architecture](#architecture)
- [Development](#development)
- [Contributing](#contributing)

---

## Mission Statement

The purpose of this project is not a Luddistic rejection of AI. It's an interesting technology with some useful applications in domains and cases where accuracy, quality, maintainability, and accountability don't matter all that much. It is a rejection of the labs, hyperscalers, investment bankers, private equity firms, and the captive media industry that has driven a three+ year hype cycle that has done significant damage to the tech ecosystem, the economy, international relations, and the lives of people and communities that have been sacrificed on a pyre of money to a false god.

In normal protests against a particular company or industry, you try to hit em where it hurts by not buying their product, in the hope that the losses will induce them to change their behavior. For the AI companies, however, just not using them doesn't really matter.  When they're already sitting at 900M "weekly average users", even if millions of people boycotted them, it would barely be a blip on the investor prospectuses they use for their absurd fundraising rounds. 

Enter the Buycott, which exploits the fundamental failure of the AI industry to come up with a product model that doesn't depend on subsidizing usage in the range of 300-2000%. That means for every $1 of usage you pay for, they're paying $3-$20, maybe more. Even on inference, and also almost certainly (although to a lesser degree atm) on the token-based plans that some providers have started rolling out. "it's ok if we lose money on every sale, we'll make it up in volume" is now the height of based thought.

That means that you can hurt them by using them in exactly the way they tell you to. You don't have to do anything nefarious, or against usage policies, or illegal. Just use their product and watch their money burn.

If you're like a lot of people in the tech industry, even if you're not using these AI tools regularly,  you have subscriptions to these services because it's sort of necessary to keep up with what's going on in the ecosystem. There's probably a lot of idle time on those subs.  Put them to use. If you're someone in a community threatened by data centers, for $20/month you can do more damage than you'd achieve via the captured political process. Whatever the source of your antipathy, for the cost of one cup of coffee per week, you can leave an oversized crater in their finances. 

Don't use this with your work accounts, that's not your money. Don't use it with token-based billing accounts without setting spend limits that you are comfortable with, and be aware that there's less certainty around the rate of subsidized consumption.

---

## What is Buycott?

<img src="images/buycott-profile.png" align="right" width="200" style="margin-left: 24px; margin-bottom: 16px;">

Buycott is a **headless container** that runs an autonomous AI software development team. You give it a product direction in plain English. It handles the rest: decomposing work into tasks, writing and testing code, reviewing its own output, and cutting versioned releases — continuously, without you lifting a finger.

It works two ways, and you can mix them per role:

- **Metered API keys** — Anthropic, OpenAI, and Gemini, billed per token.
- **Subscriptions** — drive a role through the `claude`, `codex`, or `gemini` CLI so it runs on a Claude / ChatGPT / Google plan you already pay for. This is the point: those plans are sold below cost, so putting idle subscription capacity to work burns the provider's money, not yours. See the [Mission Statement](#mission-statement).

Mix freely — e.g., a Claude PM on API, a `codex`-subscription backend, and a `gemini`-subscription copywriter. Every prompt/response exchange is logged and inspectable, with per-role token and cost tracking. A live web dashboard streams the action in real time.

The longer it runs, the more it burns. Point it at a hard problem and walk away.

<br clear="right">

---

## How it works

```
You: "Build a CRM for auto stores"
       │
       ▼
  ┌─────────┐     generates tasks      ┌──────────┐
  │   PM    │ ────────────────────────▶│  SQLite  │
  │  agent  │ ◀────────────────────── │  task DB │
  └─────────┘     reviews & approves   └──────────┘
       │                                     │
       │ delegates                           │ dequeues
       ▼                                     ▼
  ┌──────────────────────────────────────────────┐
  │              Role agents                     │
  │  backend · frontend · copywriter · custom    │
  └──────────────────────────────────────────────┘
       │
       │ writes files + runs tests
       ▼
  ┌──────────────┐      ┌───────────────────────────┐
  │   Docker     │      │  /artifacts/              │
  │  container   │─────▶│    src/                   │
  │  (ephemeral) │      │    releases/v0.1.0/       │
  └──────────────┘      └───────────────────────────┘
```

1. **PM generates tasks** — breaks the direction into concrete work items and assigns them to role agents.
2. **Role agents execute** — each agent writes code/copy/config, proposes test commands, and returns structured output.
3. **Code runs in Docker** — test commands execute in an ephemeral container. On failure, the agent retries (up to 10× before escalating to the PM).
4. **Code review gate** — a dedicated reviewer agent checks correctness, completeness, and code quality before work reaches the PM.
5. **Security review gate** — a security-focused agent scans for vulnerabilities and can require remediation before approval.
6. **PM reviews** — checks completed work against acceptance criteria; approves or rejects with specific feedback.
7. **Releases ship automatically** — after every N completed tasks the PM decides whether to cut a versioned release.

---

## Quick start

### Prerequisites

- Docker and Docker Compose v2
- Auth for at least one provider — either an API key (Anthropic, OpenAI, or Gemini) **or** a logged-in subscription CLI (`claude`, `codex`, or `gemini`)

### 1. Clone and configure

```bash
git clone <this-repo>
cd buycott
make setup        # interactive wizard
```

`make setup` walks you through picking a provider and model for each role and
handles the auth flow for each one — including the subscription CLI logins
(`claude setup-token`, `codex login`, `gemini`) that are otherwise painful to run
inside a headless container. It writes `config.yaml`, `.env`, and (when a
subscription provider is selected) a `docker-compose.override.yml` that mounts
your host CLI credentials into the container.

Prefer to edit by hand? Copy the examples instead:

```bash
cp config.example.yaml config.yaml   # edit model/provider choices
cp .env.example .env                 # add your API key(s) / tokens
```

### 2. Run

```bash
make compose-up DIRECTION="Build a CRM for auto stores"
```

Or directly with Docker Compose:

```bash
export ANTHROPIC_API_KEY=sk-ant-...
BUYCOTT_DIRECTION="Build a CRM" docker compose up -d
```

Artifacts accumulate in `./artifacts/` immediately. The pipeline never stops until you tell it to.

### 3. Watch it work

```bash
make compose-logs           # stream live logs from all containers
make compose-status         # task queue depth, active task, counts
make compose-conversation   # recent prompt/response exchanges
```

### 4. Dashboard

Open **http://localhost:8000** — live pipeline status, task list, event stream, releases, per-role token usage / cost, and full conversation logs, all updating in real time via SSE. Click any event to expand its full payload. The **Reset run** button clears all state and starts over from scratch (see [`buycott reset`](#pipeline-control)).

### No-install option: prompt packs

Don't want to run the pipeline at all? [`prompt-packs/`](prompt-packs/) contains fill-in-the-blank prompts you paste into an interactive coding-agent session (Claude Code, Codex, or the Gemini CLI) in auto-approve mode — it builds a real, open-ended project and runs until your subscription's rate limit. Zero setup, no Docker, no API key; the lowest-friction way to put a subscription to work.

---

## Deployment

### Docker Compose — recommended for single-host

Two containers, one command:

| Service | Port | Role |
|---------|------|------|
| `pipeline` | 8080 (gRPC) | Orchestration loop + gRPC control API |
| `dashboard` | 8000 (HTTP) | Web UI; reads from `pipeline` via gRPC |

```bash
make compose-setup                        # copy config + create artifacts/
make compose-up DIRECTION="Build a REST API"
make compose-logs                         # stream all logs
make compose-ps                           # service health
make compose-down                         # stop everything
make compose-status                       # buycott status inside the container
make compose-conversation                 # recent conversation logs
```

**Environment variables** (`.env` file or shell exports):

| Variable | Default | Description |
|----------|---------|-------------|
| `ANTHROPIC_API_KEY` | — | Required if using `anthropic` (API) models |
| `OPENAI_API_KEY` | — | Required if using `openai` (API) models |
| `GEMINI_API_KEY` | — | Required if using `gemini` (API) models |
| `CLAUDE_CODE_OAUTH_TOKEN` | — | Optional headless token for the `claude-code` provider (`claude setup-token`) |
| `BUYCOTT_DIRECTION` | `Build a sample web application` | Initial product direction |
| `BUYCOTT_API_PORT` | `8080` | Host port for the gRPC API |
| `BUYCOTT_DASHBOARD_PORT` | `8000` | Host port for the web dashboard |
| `ARTIFACTS_PATH` | `./artifacts` | Host path for the artifacts volume |

> [!NOTE]
> The subscription CLI providers (`codex`, `gemini-cli`) authenticate via the CLI's own login, not an env var. Run `make setup` to log in on the host and mount the credentials into the container, and uncomment the CLI install block in the `Dockerfile` so the image contains the `claude` / `codex` / `gemini` binaries. See [Supported providers](#supported-providers).

---

### Kubernetes

Buycott ships with Kubernetes manifest templates and an interactive configuration script.

#### 1. Generate manifests

```bash
make k8s-configure
```

The script prompts for namespace, image, resource limits, API keys, and other parameters, then writes fully-populated YAML to `k8s/manifests/`.

| Parameter | Default | Description |
|-----------|---------|-------------|
| `NAMESPACE` | `buycott` | Kubernetes namespace |
| `IMAGE` | `buycott:latest` | Docker image (push to your registry first) |
| `PROJECT_NAME` | `My Buycott Project` | Project name in config.yaml |
| `BUYCOTT_DIRECTION` | `Build a sample web application` | Initial direction |
| `API_PORT` | `8080` | gRPC port (pipeline ↔ dashboard) |
| `DASHBOARD_PORT` | `8000` | Dashboard HTTP port |
| `DASHBOARD_SERVICE_TYPE` | `ClusterIP` | `ClusterIP` / `NodePort` / `LoadBalancer` |
| `STORAGE_CLASS` | `standard` | PVC storage class (`""` = cluster default) |
| `ARTIFACTS_SIZE` | `10Gi` | PVC size for the artifacts volume |
| Pipeline CPU/mem requests | `500m` / `512Mi` | Resource floor |
| Pipeline CPU/mem limits | `2` / `2Gi` | Resource ceiling |
| Dashboard CPU/mem requests | `100m` / `64Mi` | Resource floor |
| Dashboard CPU/mem limits | `500m` / `256Mi` | Resource ceiling |

#### 2. Build and push the image

```bash
make docker-build IMAGE_NAME=myregistry/buycott IMAGE_TAG=v1.0.0
docker push myregistry/buycott:v1.0.0
```

#### 3. Deploy

```bash
make k8s-apply    # kubectl apply -f k8s/manifests/
make k8s-status   # kubectl get all,pvc,configmap,secret -n buycott
```

#### 4. Operate

```bash
make k8s-logs-pipeline    # stream pipeline logs
make k8s-logs-dashboard   # stream dashboard logs
make k8s-delete           # remove all resources and namespace
```

**Manifest files:**

| File | Contents |
|------|----------|
| `00-namespace.yaml` | Namespace |
| `01-configmap.yaml` | `config.yaml` content |
| `02-secrets.yaml` | API key Secret |
| `03-rbac.yaml` | ServiceAccount |
| `04-pvc.yaml` | Artifacts PersistentVolumeClaim |
| `05-pipeline.yaml` | Pipeline Deployment + ClusterIP Service |
| `06-dashboard.yaml` | Dashboard Deployment + Service |

> [!IMPORTANT]
> The pipeline `Deployment` is `replicas: 1` with `strategy: Recreate` — SQLite is `ReadWriteOnce` and the pipeline holds in-memory state. `k8s/manifests/` contains API keys and is gitignored. **Never commit it.**

> [!NOTE]
> Agent execution uses the host Docker socket (`hostPath: /var/run/docker.sock`). Cluster nodes must have Docker installed. For containerd/CRI-O clusters, replace with a DinD sidecar or a Tekton/Argo workflow runner.

---

## Configuration

```yaml
project:
  name: "My Project"
  artifacts_path: /artifacts   # where all output is written

roles:
  pm:
    provider: anthropic
    model: claude-opus-4-8

  backend:
    provider: anthropic
    model: claude-sonnet-4-6

  reviewer:                    # optional code review gate
    provider: anthropic
    model: claude-sonnet-4-6

  security:                    # optional security review gate
    provider: anthropic
    model: claude-sonnet-4-6
    scan_commands:             # static analysis tools to run before the LLM review
      - image: aquasec/trivy:latest
        cmd: trivy fs /artifacts --format table
      - image: returntocorp/semgrep:latest
        cmd: semgrep --config=auto /artifacts

  frontend:
    provider: codex            # ChatGPT subscription via the `codex` CLI (no API key)
    model: gpt-5-codex

  copywriter:
    provider: gemini-cli       # Google subscription via the `gemini` CLI (no API key)
    model: gemini-2.5-flash

  # Custom role — just needs a prompt file or inline prompt
  devops:
    provider: anthropic
    model: claude-sonnet-4-6
    system_prompt_file: /etc/buycott/prompts/devops.md

execution:
  max_retries: 10             # retries before a task escalates to the PM
  task_timeout: 5m
  docker_socket: /var/run/docker.sock
  artifacts_volume: buycott_artifacts  # Docker volume shared with executor containers (see note)
  prompts_dir: /etc/buycott/prompts
  release_check_interval: 10  # ask PM about a release every N completed tasks (0 = off)

api:
  port: 8080    # gRPC control API

dashboard:
  port: 8000    # web dashboard

api_keys:
  anthropic: ${ANTHROPIC_API_KEY}
  openai: ${OPENAI_API_KEY}
  gemini: ${GEMINI_API_KEY}
  claude_code: ${CLAUDE_CODE_OAUTH_TOKEN}   # optional, for the claude-code provider
```

> [!NOTE]
> `artifacts_volume` is the name of the Docker volume mounted into ephemeral executor containers as `/artifacts`. It's **required under the default socket-forwarding setup**: the host daemon resolves bind sources as host paths, so passing the in-container `artifacts_path` directly fails with `bind source did not exist`. The Compose default volume name is `buycott_artifacts`. Leave it empty only when the Docker daemon shares this container's filesystem.

### Supported providers

Two kinds of providers. **API-key providers** are billed per token. **Subscription providers** drive a role through a locally-installed, logged-in CLI so it runs on a plan you already pay for — the core of the [mission](#mission-statement).

| Provider | Config value | Kind | Auth |
|----------|-------------|------|------|
| Anthropic Claude | `anthropic` | API key | `ANTHROPIC_API_KEY` |
| OpenAI | `openai` | API key | `OPENAI_API_KEY` |
| Google Gemini | `gemini` | API key | `GEMINI_API_KEY` |
| Claude subscription | `claude-code` | subscription CLI | `claude setup-token` → `CLAUDE_CODE_OAUTH_TOKEN`, or an existing `claude login` |
| ChatGPT subscription | `codex` | subscription CLI | `codex login` (credentials under `~/.codex`) |
| Google subscription | `gemini-cli` | subscription CLI | `gemini` login (credentials under `~/.gemini`) |

Mix and match freely — each role can use a different provider and model.

**Subscription providers** shell out to the CLI per call, so:
- The CLI must be on the container's `PATH` — uncomment the install block in the `Dockerfile`.
- Authenticate on the host (`make setup` does this) and mount the credential directory in; the provider strips the matching API-key env var so it uses the subscription login, not metered billing.
- They hit subscription rate limits faster than API keys. The pipeline backs off automatically and surfaces throttled roles in the dashboard.

---

## Role system

### System prompts

Prompts are **loaded from `.md` files**, never hardcoded. Resolution order (first match wins):

1. Inline `system_prompt:` in config
2. `system_prompt_file:` path in role config
3. `{prompts_dir}/{role_name}.md`

```yaml
# Per-role file override
roles:
  backend:
    system_prompt_file: /my/prompts/backend.md

# Inline (highest priority)
roles:
  backend:
    system_prompt: |
      You are a Rust engineer. Use the standard JSON task output format.

# Custom directory for all roles
execution:
  prompts_dir: /my/prompts
```

### Engineer output format

All non-PM roles respond in this JSON shape (via structured tool use):

```json
{
  "narrative": "Brief explanation of approach and key decisions",
  "files": {
    "/artifacts/src/main.go": "package main\n..."
  },
  "run_image": "golang:1.22-alpine",
  "run_commands": ["cd /artifacts", "go test ./..."],
  "subtask": {
    "role": "frontend",
    "title": "Design the API contract",
    "description": "...",
    "acceptance_criteria": ["..."]
  }
}
```

`run_image` / `run_commands` are optional (leave empty for content-only tasks). `subtask` pauses the current task and spawns a blocking sub-task to another role — the result is injected back into the conversation when it completes.

### Adding a custom role

1. Add a prompt file at `prompts/my_role.md`
2. Add to config:

```yaml
roles:
  my_role:
    provider: anthropic
    model: claude-sonnet-4-6
```

3. The PM can now assign tasks with `"assigned_role": "my_role"`. No code changes needed.

---

## CLI reference

### Pipeline control

| Command | Description |
|---------|-------------|
| `buycott start --config cfg.yaml "direction"` | Start the pipeline (+ gRPC API + dashboard) |
| `buycott start --no-dashboard "direction"` | Start pipeline + gRPC API only |
| `buycott pause` | Pause after the current task completes |
| `buycott resume` | Resume a paused pipeline |
| `buycott reset` | Clear all run state (tasks, events, releases, logs) and start over |
| `buycott reset --wipe-artifacts` | Also delete generated project files under `/artifacts` |
| `buycott status` | Active task, queue depth, counts |

> [!NOTE]
> Run `buycott reset` while the pipeline is stopped (it operates on the SQLite DB directly), then `buycott start` again. From the dashboard, use the **Reset run** button instead — it stops the loop, clears state, and restarts in one step. Reset is not available over a remote `--server` connection; run it on the pipeline host.

### Inspection

| Command | Description |
|---------|-------------|
| `buycott inspect task <id>` | Full task detail (history, execution log) |
| `buycott inspect tasks [status]` | List tasks, optionally filtered by status |
| `buycott inspect releases` | List all releases |
| `buycott inspect artifacts [subpath]` | Browse the artifacts volume |
| `buycott logs` | Print the event log |
| `buycott logs --follow` | Stream events live |

### Conversation logs

```bash
buycott conversation                          # all recent exchanges
buycott conversation --role backend           # filter by role
buycott conversation --task <id-prefix>       # filter by task
buycott conversation --limit 5                # most recent N exchanges
buycott conversation --no-color | less -R     # pipe-friendly output
```

### Interactive chat

```bash
# Stream a response from any role agent
buycott chat backend "Why did you use PostgreSQL instead of SQLite?"

# Inject the exchange into the active task's conversation history
buycott chat backend "Fix the N+1 query in GetUsers" --inject
```

### Dashboard (standalone)

```bash
buycott dashboard --server localhost:8080 --port 8000
```

### Remote mode

Every command accepts `--server host:port` to operate against a remote instance:

```bash
buycott --server pipeline.internal:8080 status
buycott --server pipeline.internal:8080 logs --follow
buycott --server pipeline.internal:8080 chat pm "What's the release plan?"
```

---

## Releases

After every N completed tasks (default: 10), the PM is asked whether to cut a release. When approved:

1. Creates `/artifacts/releases/v{version}/`
2. Snapshots all of `/artifacts/` into it (excluding `.buycott/` and `releases/`)
3. Writes `RELEASE.md` with the PM's notes
4. Records the release in the state DB

```bash
buycott inspect releases
# v0.1.0   2026-06-13 14:22   /artifacts/releases/v0.1.0
# v0.2.0   2026-06-13 16:45   /artifacts/releases/v0.2.0
```

---

## Task lifecycle

```
PENDING → IN_PROGRESS → [code review] → [security review] → PENDING_REVIEW → DONE
                │                                                   │
                │                                               REJECTED ──→ PENDING
                │                                            (PM feedback injected)
                │
          (after max_retries failures)
                │
           ESCALATED ──→ PM escalation task inserted as PENDING
```

- **Code review** and **security review** gates are optional — remove `reviewer:` or `security:` from config to disable them.
- PM tasks skip `PENDING_REVIEW` (auto-approved) to prevent the PM reviewing its own work.
- Escalated tasks generate a new PM task with the full error history so the PM can decompose, reframe, or descope.

---

## Architecture

```
cmd/                        CLI entry points (Cobra)
internal/
  model/                    Shared types: Task, Release, Event, LLMLog, ExecResult, ScanResult
  config/                   YAML loader with ${ENV_VAR} expansion
  llm/                      Provider interface · API providers (Anthropic · OpenAI · Gemini)
                              CLI/subscription providers (claude-code · codex · gemini-cli)
                              LoggingProvider (records every exchange)
                              RetryingProvider (exponential backoff on 429s)
  state/                    SQLite: tasks, events, releases, pipeline_state, llm_logs
                              ClearAll / WipeArtifacts (reset support)
  executor/                 Docker socket executor (runs agent test commands)
  roles/                    Role interface · prompt loader · PM · engineers · reviewer
                              SecurityReviewerRole (static analysis + LLM synthesis)
  pipeline/                 Orchestration loop · retry logic · release cadence
  server/                   Server interface + LocalServer (in-process)
  grpcserver/               gRPC server wrapping server.Server
  grpcclient/               gRPC client implementing server.Server (for --server flag)
  dashboard/                HTTP dashboard server (SSE events · REST API · embedded HTML)
  grpcapi/                  Generated protobuf/gRPC types
prompts/                    System prompt .md files per role (pm, backend, frontend,
                              copywriter, reviewer, security)
k8s/
  templates/                Kubernetes manifest templates ({{PLACEHOLDER}} syntax)
  manifests/                Generated manifests (gitignored — contains API keys)
scripts/
  setup.sh                  Interactive provider/model/auth wizard (make setup)
  entrypoint.sh             Container entrypoint
  configure-k8s.sh          Interactive manifest generator
proto/                      gRPC service definition (regenerate with make proto)
Makefile                    Build, compose, k8s, setup, and proto targets
```

State persists in `{artifacts_path}/.buycott/state.db`. Kill and restart the container — the pipeline resumes exactly where it left off.

---

## Development

```bash
# Build
GOTOOLCHAIN=auto go build ./...
GOTOOLCHAIN=auto go vet ./...
GOTOOLCHAIN=auto go test ./...

# Or via Make
make build
make vet
make test

# Docker image
make docker-build

# Local run (requires config.yaml and accessible prompts)
make run DIRECTION="Build something"
```

> [!NOTE]
> `GOTOOLCHAIN=auto` is required — `go.mod` specifies Go 1.25 (pulled up by transitive Docker SDK → OpenTelemetry dependencies) but your local toolchain may be older. The flag downloads the right version automatically.

See [`CLAUDE.md`](CLAUDE.md) for codebase conventions.

---

## Contributing

Bug fixes, new LLM providers, new built-in roles, and documentation improvements are all welcome.

See **[CONTRIBUTING.md](CONTRIBUTING.md)** for:
- Dev environment setup
- Package dependency rules (no circular imports)
- How to add a new LLM provider
- How to add a new built-in role
- Database migration conventions
- Test patterns and conventions
- PR checklist

For security vulnerabilities, please report privately rather than opening a public issue.
