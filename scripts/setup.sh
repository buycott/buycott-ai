#!/usr/bin/env bash
#
# Interactive setup for Buycott.
#
# Walks you through picking a provider + model for each agent role, then handles
# the auth flow for each provider you chose — including the CLI/subscription
# logins (claude-code / codex / gemini-cli) that are painful to run by exec'ing
# into a headless container. It writes:
#
#   config.yaml                   roles + execution + api_keys
#   .env                          API keys, subscription tokens, direction, ports
#   docker-compose.override.yml   mounts host CLI credentials into the container
#                                 (only when a CLI/subscription provider is used)
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(dirname "$SCRIPT_DIR")"

# ── curated provider/model sets ───────────────────────────────────────────────
ALL_PROVIDERS=(anthropic openai gemini claude-code codex gemini-cli)

provider_desc() {
  case "$1" in
    anthropic)   echo "Anthropic API (metered, needs API key)";;
    openai)      echo "OpenAI API (metered, needs API key)";;
    gemini)      echo "Google Gemini API (metered, needs API key)";;
    claude-code) echo "Claude subscription via the 'claude' CLI";;
    codex)       echo "ChatGPT subscription via the 'codex' CLI";;
    gemini-cli)  echo "Google account via the 'gemini' CLI";;
  esac
}

models_for() {
  case "$1" in
    anthropic|claude-code) echo "claude-opus-4-8 claude-sonnet-4-6 claude-haiku-4-5";;
    openai)                echo "gpt-4o gpt-4o-mini gpt-4-turbo";;
    gemini)                echo "gemini-1.5-pro gemini-1.5-flash gemini-2.0-flash";;
    codex)                 echo "gpt-5-codex gpt-5 o4-mini";;
    gemini-cli)            echo "gemini-2.5-pro gemini-2.5-flash";;
  esac
}

# roles offered (pm is required)
ROLES=(pm backend frontend copywriter reviewer)
role_desc() {
  case "$1" in
    pm)         echo "Product manager — plans tasks, reviews, cuts releases (required)";;
    backend)    echo "Backend engineer";;
    frontend)   echo "Frontend engineer";;
    copywriter) echo "Copywriter";;
    reviewer)   echo "Code reviewer — quality gate before PM review (optional)";;
  esac
}

# ── prompt helpers (prompts go to stderr; result returned in globals) ──────────
CHOICE=""
prompt_choice() { # prompt_choice "Question" opt1 opt2 ...
  local q="$1"; shift
  local opts=("$@") i n
  printf '\n%s\n' "$q" >&2
  for i in "${!opts[@]}"; do printf '  %2d) %s\n' "$((i + 1))" "${opts[$i]}" >&2; done
  while true; do
    read -rp "  > " n
    if [[ "$n" =~ ^[0-9]+$ ]] && (( n >= 1 && n <= ${#opts[@]} )); then
      CHOICE="${opts[$((n - 1))]}"; return 0
    fi
    echo "  Please enter a number between 1 and ${#opts[@]}." >&2
  done
}

confirm() { # confirm "Question" [default y|n]
  local q="$1" def="${2:-n}" ans hint="[y/N]"
  [[ "$def" == y ]] && hint="[Y/n]"
  read -rp "$q $hint " ans
  ans="${ans:-$def}"
  [[ "$ans" =~ ^[Yy] ]]
}

read_secret() { # read_secret "Prompt" -> echoes value on stdout
  local p="$1" v
  read -rsp "$p" v >&2; echo >&2
  printf '%s' "$v"
}

# ── state ─────────────────────────────────────────────────────────────────────
declare -A ROLE_PROVIDER ROLE_MODEL ENV_VARS PROVIDER_USED
MOUNT_CODEX=0 MOUNT_GEMINI=0
CONFIGURED_ROLES=()

echo "============================================"
echo " Buycott setup"
echo "============================================"
echo "Pick a provider and model for each agent role. Roles you skip are left"
echo "out of config.yaml."

# ── role selection ────────────────────────────────────────────────────────────
for role in "${ROLES[@]}"; do
  echo
  echo "── ${role} ──  $(role_desc "$role")"
  if [[ "$role" != pm ]]; then
    confirm "Configure the '${role}' role?" y || continue
  fi

  menu=()
  for p in "${ALL_PROVIDERS[@]}"; do menu+=("$p — $(provider_desc "$p")"); done
  prompt_choice "Provider for '${role}':" "${menu[@]}"
  provider="${CHOICE%% *}"

  mapfile -t mlist < <(printf '%s\n' $(models_for "$provider"))
  mlist+=("custom (enter a model id)")
  prompt_choice "Model for '${role}' (${provider}):" "${mlist[@]}"
  if [[ "$CHOICE" == custom* ]]; then
    read -rp "  Enter model id: " model
  else
    model="$CHOICE"
  fi

  ROLE_PROVIDER[$role]="$provider"
  ROLE_MODEL[$role]="$model"
  PROVIDER_USED[$provider]=1
  CONFIGURED_ROLES+=("$role")
  echo "  ✓ ${role}: ${provider} / ${model}"
done

# ── auth per distinct provider ────────────────────────────────────────────────
echo
echo "============================================"
echo " Authentication"
echo "============================================"

for provider in "${ALL_PROVIDERS[@]}"; do
  [[ -n "${PROVIDER_USED[$provider]:-}" ]] || continue
  echo
  echo "── ${provider} ──"
  case "$provider" in
    anthropic)
      ENV_VARS[ANTHROPIC_API_KEY]="$(read_secret "  Anthropic API key (sk-ant-api03-…): ")" ;;
    openai)
      ENV_VARS[OPENAI_API_KEY]="$(read_secret "  OpenAI API key (sk-…): ")" ;;
    gemini)
      ENV_VARS[GEMINI_API_KEY]="$(read_secret "  Google Gemini API key: ")" ;;

    claude-code)
      if command -v claude >/dev/null 2>&1 && confirm "  Run 'claude setup-token' now to mint a headless token?" y; then
        echo "  Launching 'claude setup-token' (follow the login prompts)…"
        tok="$(claude setup-token | tr -d '[:space:]' || true)"
        if [[ -n "$tok" ]]; then
          ENV_VARS[CLAUDE_CODE_OAUTH_TOKEN]="$tok"
          echo "  ✓ Captured CLAUDE_CODE_OAUTH_TOKEN."
        else
          echo "  ! No token captured — paste it manually below."
          ENV_VARS[CLAUDE_CODE_OAUTH_TOKEN]="$(read_secret "  CLAUDE_CODE_OAUTH_TOKEN: ")"
        fi
      else
        echo "  Get a token on a machine with the 'claude' CLI: run 'claude setup-token'."
        ENV_VARS[CLAUDE_CODE_OAUTH_TOKEN]="$(read_secret "  CLAUDE_CODE_OAUTH_TOKEN (blank to skip): ")"
      fi ;;

    codex)
      if command -v codex >/dev/null 2>&1; then
        if confirm "  Run 'codex login' now (opens a browser flow on THIS host)?" y; then
          codex login || echo "  ! codex login did not complete — rerun 'codex login' later."
        fi
        if [[ -d "$HOME/.codex" ]]; then
          MOUNT_CODEX=1
          echo "  ✓ Will mount $HOME/.codex into the container."
        else
          echo "  ! $HOME/.codex not found yet — run 'codex login', then re-run this setup."
        fi
      else
        echo "  The 'codex' CLI isn't installed here. Install it (npm i -g @openai/codex),"
        echo "  run 'codex login', then re-run this setup to mount the credentials."
      fi ;;

    gemini-cli)
      if command -v gemini >/dev/null 2>&1; then
        echo "  Authenticate the gemini CLI by running it once and completing the Google login."
        if confirm "  Launch 'gemini' now to log in?" y; then
          gemini </dev/null || true
        fi
        if [[ -d "$HOME/.gemini" ]]; then
          MOUNT_GEMINI=1
          echo "  ✓ Will mount $HOME/.gemini into the container."
        else
          echo "  ! $HOME/.gemini not found yet — log in with 'gemini', then re-run this setup."
        fi
      else
        echo "  The 'gemini' CLI isn't installed here. Install it (npm i -g @google/gemini-cli),"
        echo "  log in, then re-run this setup to mount the credentials."
      fi ;;
  esac
done

# ── product direction ─────────────────────────────────────────────────────────
echo
read -rp "Product direction (what should the agents build?): " DIRECTION || true
DIRECTION="${DIRECTION:-Build a sample web application}"

# ── write config.yaml ─────────────────────────────────────────────────────────
backup() {
  if [[ -f "$1" ]]; then
    cp "$1" "$1.bak"
    echo "  (backed up $(basename "$1") → $(basename "$1").bak)"
  fi
}

CONFIG="$ROOT/config.yaml"
backup "$CONFIG"
{
  echo "# Generated by scripts/setup.sh — edit freely."
  echo "project:"
  echo "  name: \"My Project\""
  echo "  artifacts_path: /artifacts"
  echo
  echo "roles:"
  for role in "${CONFIGURED_ROLES[@]}"; do
    echo "  ${role}:"
    echo "    provider: ${ROLE_PROVIDER[$role]}"
    echo "    model: ${ROLE_MODEL[$role]}"
  done
  echo
  echo "execution:"
  echo "  max_retries: 10"
  echo "  task_timeout: 5m"
  echo "  docker_socket: /var/run/docker.sock"
  echo "  artifacts_volume: buycott_artifacts"
  echo
  echo "api:"
  echo "  port: 8080"
  echo "dashboard:"
  echo "  port: 8000"
  echo
  echo "api_keys:"
  [[ -n "${PROVIDER_USED[anthropic]:-}" ]]   && echo "  anthropic: \${ANTHROPIC_API_KEY}"
  [[ -n "${PROVIDER_USED[openai]:-}" ]]       && echo "  openai: \${OPENAI_API_KEY}"
  [[ -n "${PROVIDER_USED[gemini]:-}" ]]       && echo "  gemini: \${GEMINI_API_KEY}"
  [[ -n "${PROVIDER_USED[claude-code]:-}" ]]  && echo "  claude_code: \${CLAUDE_CODE_OAUTH_TOKEN}"
} > "$CONFIG"
echo "==> Wrote $CONFIG"

# ── write .env ────────────────────────────────────────────────────────────────
ENVFILE="$ROOT/.env"
backup "$ENVFILE"
{
  echo "# Generated by scripts/setup.sh. Loaded automatically by docker compose."
  for k in ANTHROPIC_API_KEY OPENAI_API_KEY GEMINI_API_KEY CLAUDE_CODE_OAUTH_TOKEN; do
    [[ -n "${ENV_VARS[$k]:-}" ]] && echo "${k}=${ENV_VARS[$k]}"
  done
  echo "BUYCOTT_DIRECTION=${DIRECTION}"
  echo "BUYCOTT_API_PORT=8080"
  echo "BUYCOTT_DASHBOARD_PORT=8000"
  echo "ARTIFACTS_PATH=./artifacts"
} > "$ENVFILE"
chmod 600 "$ENVFILE"
echo "==> Wrote $ENVFILE (chmod 600)"

# ── write compose override for CLI credential mounts ──────────────────────────
if (( MOUNT_CODEX || MOUNT_GEMINI )); then
  OVERRIDE="$ROOT/docker-compose.override.yml"
  backup "$OVERRIDE"
  {
    echo "# Generated by scripts/setup.sh — mounts host CLI credentials into the"
    echo "# pipeline container so subscription logins work headlessly."
    echo "services:"
    echo "  pipeline:"
    echo "    volumes:"
    (( MOUNT_CODEX ))  && echo "      - ${HOME}/.codex:/root/.codex"
    (( MOUNT_GEMINI )) && echo "      - ${HOME}/.gemini:/root/.gemini"
  } > "$OVERRIDE"
  echo "==> Wrote $OVERRIDE"
fi

# ── summary & next steps ──────────────────────────────────────────────────────
echo
echo "============================================"
echo " Done"
echo "============================================"
echo "Roles configured:"
for role in "${CONFIGURED_ROLES[@]}"; do
  printf '  %-11s %s / %s\n' "$role" "${ROLE_PROVIDER[$role]}" "${ROLE_MODEL[$role]}"
done

if (( MOUNT_CODEX || MOUNT_GEMINI )) || [[ -n "${PROVIDER_USED[claude-code]:-}" ]]; then
  echo
  echo "NOTE: you selected CLI/subscription provider(s). The Docker image must"
  echo "contain those CLIs — uncomment the CLI install block in the Dockerfile and"
  echo "rebuild ('docker compose build' or 'make docker-build') before starting."
fi

echo
echo "Next:"
echo "  docker compose up        # build/run pipeline + dashboard"
echo "  open http://localhost:8000"
