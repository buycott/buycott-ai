# Buycott — Multi-model Task Pipeline

## Mission Statement

The purpose of this project is not a Luddistic rejection of AI. It's an interesting technology with some useful applications in domains and cases where accuracy, quality, maintainability, and accountability don't matter all that much. It is a rejection of the labs, hyperscalers, investment bankers, private equity firms, and the captive media industry that has driven a three+ year hype cycle that has done significant damage to the tech ecosystem, the economy, international relations, and the lives of people and communities that have been sacrificed on a pyre of money to a false god.

In normal protests against a particular company or industry, you try to hit em where it hurts by not buying their product, in the hope that the losses will induce them to change their behavior. For the AI companies, however, just not using them doesn't really matter.  When they're already sitting at 900M "weekly average users", even if millions of people boycotted them, it would barely be a blip on the investor prospectuses they use for their absurd fundraising rounds. 

Enter the Buycott, which exploits the fundamental failure of the AI industry to come up with a product model that doesn't depend on subsidizing usage in the range of 300-2000%. That means for every $1 of usage you pay for, they're paying $3-$20, maybe more. Even on inference, and also almost certainly (although to a lesser degree atm) on the token-based plans that some providers have started rolling out. "it's ok if we lose money on every sale, we'll make it up in volume" is now the height of based thought.

That means that you can hurt them by using them in exactly the way they tell you to. You don't have to do anything nefarious, or against usage policies, or illegal. Just use their product and watch their money burn.

If you're like a lot of people in the tech industry, even if you're not using these AI tools regularly,  you have subscriptions to these services because it's sort of necessary to keep up with what's going on in the ecosystem. There's probably a lot of idle time on those subs.  Put them to use. If you're someone in a community threatened by data centers, for $20/month you can do more damage than you'd achieve via the captured political process. Whatever the source of your antipathy, for the cost of one cup of coffee per week, you can leave an oversized crater in their finances. 

Don't use this with your work accounts, that's not your money. Don't use it with token-based billing accounts without setting spend limits that you are comfortable with, and be aware that there's less certainty around the rate of subsidized consumption.

---

Buycott is a headless container that orchestrates a team of LLM agents to autonomously build software. You give it a product direction; it generates tasks, writes and tests code, reviews its own work, and ships versioned releases — continuously, without human intervention.

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
  │  Docker      │      │  /artifacts/              │
  │  container   │─────▶│    src/                   │
  │  (ephemeral) │      │    releases/v0.1.0/       │
  └──────────────┘      └───────────────────────────┘
```

1. **PM generates tasks** — breaks the product direction into concrete work items and assigns them to role agents.
2. **Role agents execute** — each agent writes code/copy/config, proposes test commands, and returns structured output.
3. **Code runs in Docker** — Buycott executes test commands inside an ephemeral container. On failure the agent retries (up to 10 times before escalating to the PM).
4. **PM reviews** — checks completed work against acceptance criteria; approves or rejects with feedback.
5. **Releases ship automatically** — after every N completed tasks the PM decides whether to cut a versioned release.

---

## Quick start

### Prerequisites

- Docker and Docker Compose v2
- API key(s) for at least one provider (Anthropic, OpenAI, or Gemini)

### 1. Clone and configure

```bash
git clone <this-repo>
cd buycott
cp config.example.yaml config.yaml   # edit model/provider choices
cp .env.example .env                 # add your API key(s)
```

### 2. Run

```bash
make compose-up DIRECTION="Build a CRM for auto stores"
```

Or using Docker Compose directly:

```bash
export ANTHROPIC_API_KEY=sk-ant-...
BUYCOTT_DIRECTION="Build a CRM" docker compose up -d
```

The pipeline starts immediately. Artifacts accumulate in `./artifacts/`.

### 3. Observe

```bash
# Live logs from both containers
make compose-logs

# Pipeline status (tasks, queue depth)
make compose-status

# Recent conversation logs (prompt/response exchanges)
make compose-conversation

# Or exec in directly
docker compose exec pipeline buycott status --config /etc/buycott/config.yaml
docker compose exec pipeline buycott logs --follow --config /etc/buycott/config.yaml
```

### 4. Dashboard

Open **http://localhost:8000** in your browser. The dashboard shows:
- Live pipeline status and active task
- Full task list with status badges
- Event stream (SSE-powered, updates in real time)
- Releases
- Conversation logs (every prompt/response exchange, viewable as threaded conversations)

---

## Deployment

### Docker Compose (recommended for single-host)

The default `docker-compose.yml` runs two services:

| Service | Port | Role |
|---------|------|------|
| `pipeline` | 8080 (gRPC) | Runs the orchestration loop; exposes the gRPC control API |
| `dashboard` | 8000 (HTTP) | Web UI; reads from `pipeline` via gRPC |

```bash
# First-time setup
make compose-setup        # copies config.example.yaml and creates artifacts/

# Start
make compose-up DIRECTION="Build a REST API"

# Useful operations
make compose-logs         # stream logs from all services
make compose-ps           # show service health
make compose-down         # stop everything
make compose-status       # run buycott status inside the pipeline container
make compose-conversation # view recent conversation logs
```

**Environment variables** (`.env` file or shell exports):

| Variable | Default | Description |
|----------|---------|-------------|
| `ANTHROPIC_API_KEY` | — | Required if using Anthropic models |
| `OPENAI_API_KEY` | — | Required if using OpenAI models |
| `GEMINI_API_KEY` | — | Required if using Gemini models |
| `BUYCOTT_DIRECTION` | `Build a sample web application` | Initial product direction |
| `MTP_API_PORT` | `8080` | Host port for the gRPC API |
| `MTP_DASHBOARD_PORT` | `8000` | Host port for the web dashboard |
| `ARTIFACTS_PATH` | `./artifacts` | Host path for the artifacts volume |

---

### Kubernetes

Buycott ships with Kubernetes manifest templates and an interactive configuration script.

#### 1. Generate manifests

```bash
make k8s-configure
```

The script prompts for namespace, image, resource limits, API keys, and other parameters, then writes fully-populated YAML to `k8s/manifests/`.

Configurable parameters:

| Parameter | Default | Description |
|-----------|---------|-------------|
| `| `NAMESPACE` | `buycott`  | Kubernetes namespace |
| `| `IMAGE` | `buycott:latest`  | Docker image (push to your registry first) |
| `PROJECT_NAME` | `My Buycott Project` | Project name in config.yaml |
| `BUYCOTT_DIRECTION` | `Build a sample web application` | Initial direction |
| `API_PORT` | `8080` | gRPC port (pipeline ↔ dashboard) |
| `DASHBOARD_PORT` | `8000` | Dashboard HTTP port |
| `DASHBOARD_SERVICE_TYPE` | `ClusterIP` | `ClusterIP` / `NodePort` / `LoadBalancer` |
| `STORAGE_CLASS` | `standard` | PVC storage class (`""` = cluster default) |
| `ARTIFACTS_SIZE` | `10Gi` | PVC size for the artifacts volume |
| Pipeline CPU/memory requests & limits | `500m / 2` / `512Mi / 2Gi` | Resource constraints |
| Dashboard CPU/memory requests & limits | `100m / 500m` / `64Mi / 256Mi` | Resource constraints |

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

**Architecture notes:**
- The pipeline `Deployment` is forced to `replicas: 1` with `strategy: Recreate` because the SQLite DB is `ReadWriteOnce` and the pipeline holds in-memory state.
- Docker agent execution uses `hostPath: /var/run/docker.sock`. This requires the cluster nodes to have Docker installed. For clusters using containerd/CRI-O, replace it with a DinD sidecar or a Tekton/Argo workflow runner.
- `k8s/manifests/` contains API keys — it is in `.gitignore`. Never commit it.

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

  frontend:
    provider: openai
    model: gpt-4o

  copywriter:
    provider: gemini
    model: gemini-1.5-pro

  # Custom role — just needs a prompt file or inline prompt
  devops:
    provider: anthropic
    model: claude-sonnet-4-6
    system_prompt_file: /etc/buycott/prompts/devops.md

execution:
  max_retries: 10            # retries before a task is escalated to the PM
  task_timeout: 5m
  docker_socket: /var/run/docker.sock
  prompts_dir: /etc/buycott/prompts
  release_check_interval: 10  # ask PM about a release every N completed tasks (0 = off)

api:
  port: 8080   # gRPC control API

dashboard:
  port: 8000   # web dashboard

api_keys:
  anthropic: ${ANTHROPIC_API_KEY}
  openai: ${OPENAI_API_KEY}
  gemini: ${GEMINI_API_KEY}
```

### Supported providers

| Provider | Config value | Env var |
|----------|-------------|---------|
| Anthropic Claude | `anthropic` | `ANTHROPIC_API_KEY` |
| OpenAI | `openai` | `OPENAI_API_KEY` |
| Google Gemini | `gemini` | `GEMINI_API_KEY` |

---

## Role system prompts

System prompts are **loaded from files**, not hardcoded. Default: `/etc/buycott/prompts/{role_name}.md`.

```yaml
# Per-role file override
roles:
  backend:
    system_prompt_file: /my/prompts/backend.md

# Inline (highest priority)
roles:
  backend:
    system_prompt: |
      You are a Rust engineer. Respond with the standard JSON task output format.

# Custom directory for all roles
execution:
  prompts_dir: /my/prompts
```

**Resolution order** (first match wins):
1. Inline `system_prompt` in config
2. `system_prompt_file` path in role config
3. `{prompts_dir}/{role_name}.md`

### Role output format

All non-PM roles respond in this JSON shape:

```json
{
  "narrative": "Brief explanation of approach",
  "files": {
    "/artifacts/src/main.go": "package main\n..."
  },
  "run_image": "golang:1.22-alpine",
  "run_commands": ["cd /artifacts", "go test ./..."]
}
```

Leave `run_image` and `run_commands` empty for tasks that don't need execution (e.g., copywriter tasks).

---

## Adding a custom role

1. Add a prompt file at `prompts/my_role.md`
2. Add the role to your config:

```yaml
roles:
  my_role:
    provider: anthropic
    model: claude-sonnet-4-6
    # system_prompt_file defaults to {prompts_dir}/my_role.md
```

3. The PM can now assign tasks with `"assigned_role": "my_role"`.

No code changes needed.

---

## CLI reference

### Pipeline

| Command | Description |
|---------|-------------|
| `buycott start --config cfg.yaml "direction"` | Start the pipeline (+ gRPC API + dashboard) |
| `buycott start --no-dashboard "direction"` | Start pipeline + gRPC API only |
| `buycott pause` | Pause after the current task completes |
| `buycott resume` | Resume the pipeline |
| `buycott status` | Active task, queue depth, counts |

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
# Show recent prompt/response exchanges (all roles)
buycott conversation

# Filter by role
buycott conversation --role backend --limit 5

# Filter by task
buycott conversation --task <id-prefix>

# Disable ANSI colors (for piping to a pager)
buycott conversation --no-color | less -R
```

### Interactive chat

```bash
# Send a message to a role agent and stream its response
buycott chat backend "Why did you use PostgreSQL instead of SQLite?"

# Inject the exchange into the active task's conversation history
buycott chat backend "Fix the N+1 query in GetUsers" --inject
```

### Dashboard

```bash
# Start just the dashboard, connecting to a running pipeline
buycott dashboard --server localhost:8080 --port 8000

# Start on a custom port (reads config for default)
buycott dashboard --port 9000
```

### Remote mode

All commands accept `--server host:port` to operate against a remote Buycott instance:

```bash
buycott --server pipeline.internal:8080 status
buycott --server pipeline.internal:8080 logs --follow
buycott --server pipeline.internal:8080 chat pm "What's the release plan?"
```

---

## Releases

The PM is prompted for a release after every N completed tasks (default: 10). When approved:
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
PENDING → IN_PROGRESS → PENDING_REVIEW → DONE
                │               │
                │           REJECTED ──→ PENDING (retry, feedback appended)
                │
          (after 10 failures)
                │
           ESCALATED ──→ PM escalation task inserted
```

PM tasks skip `PENDING_REVIEW` (auto-approved) to avoid the PM reviewing its own work.

---

## Architecture

```
cmd/                        CLI entry points (Cobra)
internal/
  model/                    Shared types: Task, Release, Event, LLMLog, ExecResult
  config/                   YAML loader with ${ENV_VAR} expansion
  llm/                      Provider interface + Anthropic / OpenAI / Gemini + LoggingProvider
  state/                    SQLite: tasks, events, releases, pipeline_state, llm_logs
  executor/                 Docker socket executor (runs agent test commands)
  roles/                    Role interface, prompt loader, PM + built-in roles
  pipeline/                 Orchestration loop, retry logic, release cadence
  server/                   Server interface + LocalServer (in-process)
  grpcserver/               gRPC server wrapping server.Server
  grpcclient/               gRPC client implementing server.Server (for --server flag)
  dashboard/                HTTP dashboard server (SSE events, REST API, embedded HTML)
  grpcapi/                  Generated protobuf/gRPC types
prompts/                    Default system prompt .md files per role
k8s/
  templates/                Kubernetes manifest templates ({{PLACEHOLDER}} syntax)
  manifests/                Generated manifests (gitignored — contains API keys)
scripts/
  entrypoint.sh             Container entrypoint
  configure-k8s.sh          Interactive manifest generator
Makefile                    Build, compose, and k8s targets
```

State is persisted in `{artifacts_path}/.buycott/state.db` (SQLite). Killing and restarting the container resumes from where it left off.

---

## Development

```bash
# Build
GOTOOLCHAIN=auto go build ./...

# Vet
GOTOOLCHAIN=auto go vet ./...

# Or use Make
make build
make vet

# Local run (prompts must be accessible)
make run DIRECTION="Build something"
```

`GOTOOLCHAIN=auto` is required because `go.mod` specifies Go 1.25 (bumped by transitive Docker SDK dependencies) while the locally installed toolchain may be older.

See `CLAUDE.md` for codebase conventions relevant to AI-assisted development.
