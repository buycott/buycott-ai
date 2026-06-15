<p align="center">
  <img src="images/buycott-banner.png" alt="Buycott — Buy it. Hurt them." width="100%">
</p>

<p align="center">
  <strong>Every token costs them money. Every task burns their runway.<br>Ship real software. Do it automatically. Watch the money disappear.</strong>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-1.25-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Go 1.25">
  <img src="https://img.shields.io/badge/Docker-ready-2496ED?style=flat-square&logo=docker&logoColor=white" alt="Docker">
  <img src="https://img.shields.io/badge/providers-Anthropic_·_OpenAI_·_Gemini-FF4500?style=flat-square" alt="LLM Providers">
  <img src="https://img.shields.io/badge/human_intervention-none-black?style=flat-square" alt="Autonomous">
</p>

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

It supports **Anthropic, OpenAI, and Gemini** out of the box. You can mix providers per role — e.g., a Claude PM, GPT-4o engineers, and a Gemini copywriter. Every prompt/response exchange is logged and inspectable. A live web dashboard streams the action in real time.

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

Open **http://localhost:8000** — live pipeline status, task list, event stream, releases, and full conversation logs, all updating in real time via SSE.

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
| `ANTHROPIC_API_KEY` | — | Required if using Anthropic models |
| `OPENAI_API_KEY` | — | Required if using OpenAI models |
| `GEMINI_API_KEY` | — | Required if using Gemini models |
| `BUYCOTT_DIRECTION` | `Build a sample web application` | Initial product direction |
| `BUYCOTT_API_PORT` | `8080` | Host port for the gRPC API |
| `BUYCOTT_DASHBOARD_PORT` | `8000` | Host port for the web dashboard |
| `ARTIFACTS_PATH` | `./artifacts` | Host path for the artifacts volume |

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
  max_retries: 10             # retries before a task escalates to the PM
  task_timeout: 5m
  docker_socket: /var/run/docker.sock
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
```

### Supported providers

| Provider | Config value | Env var |
|----------|-------------|---------|
| Anthropic Claude | `anthropic` | `ANTHROPIC_API_KEY` |
| OpenAI | `openai` | `OPENAI_API_KEY` |
| Google Gemini | `gemini` | `GEMINI_API_KEY` |

Mix and match freely — each role can use a different provider and model.

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
  llm/                      Provider interface · Anthropic · OpenAI · Gemini
                              LoggingProvider (records every exchange)
                              RetryingProvider (exponential backoff on 429s)
  state/                    SQLite: tasks, events, releases, pipeline_state, llm_logs
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
  entrypoint.sh             Container entrypoint
  configure-k8s.sh          Interactive manifest generator
Makefile                    Build, compose, and k8s targets
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
