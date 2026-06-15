#!/usr/bin/env bash
# configure-k8s.sh — Interactive generator for Buycott Kubernetes manifests.
#
# Usage:
#   ./scripts/configure-k8s.sh
#   make k8s-configure
#
# Reads values interactively (with defaults), substitutes them into
# k8s/templates/*.yaml, and writes the results to k8s/manifests/.
#
# The generated manifests contain API keys — do NOT commit them.
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$SCRIPT_DIR")"
TEMPLATES="$REPO_ROOT/k8s/templates"
OUTPUT="$REPO_ROOT/k8s/manifests"
CONFIG_CACHE="$REPO_ROOT/k8s/.config"

# ── Colors ────────────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'
CYAN='\033[0;36m'; BOLD='\033[1m'; RESET='\033[0m'

header() {
  echo ""
  echo -e "${BOLD}${CYAN}╔════════════════════════════════════════╗${RESET}"
  echo -e "${BOLD}${CYAN}║  Buycott — Kubernetes Manifest Generator   ║${RESET}"
  echo -e "${BOLD}${CYAN}╚════════════════════════════════════════╝${RESET}"
  echo ""
}

section() { echo -e "\n${BOLD}$1${RESET}"; }

# Prompt for a value, using a cached value if present, else show the default.
# Usage: ask VAR_NAME "Description" "default"
ask() {
  local var="$1" desc="$2" default="$3"
  # If already set (from cache), skip prompting.
  if [[ -n "${!var:-}" ]]; then
    echo -e "  ${CYAN}${var}${RESET} = ${!var}  ${YELLOW}(cached)${RESET}"
    return
  fi
  local prompt
  if [[ -n "$default" ]]; then
    prompt="  ${BOLD}${desc}${RESET} [${YELLOW}${default}${RESET}]: "
  else
    prompt="  ${BOLD}${desc}${RESET}: "
  fi
  local value
  while true; do
    read -rp "$(echo -e "$prompt")" value
    value="${value:-$default}"
    if [[ -n "$value" ]]; then
      printf -v "$var" '%s' "$value"
      break
    fi
    echo -e "  ${RED}Required — please enter a value.${RESET}"
  done
}

# Ask for a secret (no echo, don't save to cache file).
ask_secret() {
  local var="$1" desc="$2"
  if [[ -n "${!var:-}" ]]; then
    echo -e "  ${CYAN}${var}${RESET} = ${YELLOW}(set from environment or cache)${RESET}"
    return
  fi
  local value
  read -rsp "$(echo -e "  ${BOLD}${desc}${RESET}: ")" value
  echo ""
  printf -v "$var" '%s' "$value"
}

# ── Load cached non-secret values ─────────────────────────────────────────────
if [[ -f "$CONFIG_CACHE" ]]; then
  echo -e "${YELLOW}Found cached configuration at $CONFIG_CACHE${RESET}"
  read -rp "$(echo -e "Load cached values? [${BOLD}Y${RESET}/n]: ")" _load
  if [[ "${_load:-Y}" =~ ^[Yy]$ ]]; then
    # shellcheck disable=SC1090
    source "$CONFIG_CACHE"
    echo -e "${GREEN}Loaded.${RESET}"
  fi
fi

header

# ── Cluster & image ───────────────────────────────────────────────────────────
section "Cluster & image"
ask NAMESPACE        "Kubernetes namespace"                     "buycott"
ask IMAGE            "Docker image (registry/name:tag)"         "buycott:latest"

# ── Project ───────────────────────────────────────────────────────────────────
section "Project"
ask PROJECT_NAME     "Project name"                             "My Buycott Project"
ask BUYCOTT_DIRECTION    "Product direction (what should Buycott build?)" "Build a sample web application"

# ── Networking ────────────────────────────────────────────────────────────────
section "Networking"
ask API_PORT              "gRPC API port"                       "8080"
ask DASHBOARD_PORT        "Dashboard HTTP port"                 "8000"
ask DASHBOARD_SERVICE_TYPE "Dashboard Service type (ClusterIP/NodePort/LoadBalancer)" "ClusterIP"

# ── Storage ───────────────────────────────────────────────────────────────────
section "Storage"
ask STORAGE_CLASS    "PVC storage class ('standard' for minikube, 'default' for most clusters)" "standard"
ask ARTIFACTS_SIZE   "Artifacts PVC size"                       "10Gi"

# ── Pipeline resources ────────────────────────────────────────────────────────
section "Pipeline pod resources"
ask PIPELINE_CPU_REQUEST    "CPU request"     "500m"
ask PIPELINE_CPU_LIMIT      "CPU limit"       "2"
ask PIPELINE_MEMORY_REQUEST "Memory request"  "512Mi"
ask PIPELINE_MEMORY_LIMIT   "Memory limit"    "2Gi"

# ── Dashboard resources ───────────────────────────────────────────────────────
section "Dashboard pod resources"
ask DASHBOARD_REPLICAS        "Replica count"  "1"
ask DASHBOARD_CPU_REQUEST     "CPU request"    "100m"
ask DASHBOARD_CPU_LIMIT       "CPU limit"      "500m"
ask DASHBOARD_MEMORY_REQUEST  "Memory request" "64Mi"
ask DASHBOARD_MEMORY_LIMIT    "Memory limit"   "256Mi"

# ── API keys (secrets — not cached) ──────────────────────────────────────────
section "API keys"
echo -e "  ${YELLOW}These are written into k8s/manifests/02-secrets.yaml.${RESET}"
echo -e "  ${YELLOW}Do NOT commit that file to version control.${RESET}"
echo ""
ask_secret ANTHROPIC_API_KEY "Anthropic API key (required)"
ask_secret OPENAI_API_KEY    "OpenAI API key    (leave blank if unused)"
ask_secret GEMINI_API_KEY    "Gemini API key    (leave blank if unused)"

# ── Save non-secret cache ─────────────────────────────────────────────────────
mkdir -p "$(dirname "$CONFIG_CACHE")"
cat > "$CONFIG_CACHE" <<EOF
# Buycott Kubernetes configuration cache (no secrets).
# Sourced by configure-k8s.sh on the next run to pre-fill defaults.
NAMESPACE="$NAMESPACE"
IMAGE="$IMAGE"
PROJECT_NAME="$PROJECT_NAME"
BUYCOTT_DIRECTION="$BUYCOTT_DIRECTION"
API_PORT="$API_PORT"
DASHBOARD_PORT="$DASHBOARD_PORT"
DASHBOARD_SERVICE_TYPE="$DASHBOARD_SERVICE_TYPE"
STORAGE_CLASS="$STORAGE_CLASS"
ARTIFACTS_SIZE="$ARTIFACTS_SIZE"
PIPELINE_CPU_REQUEST="$PIPELINE_CPU_REQUEST"
PIPELINE_CPU_LIMIT="$PIPELINE_CPU_LIMIT"
PIPELINE_MEMORY_REQUEST="$PIPELINE_MEMORY_REQUEST"
PIPELINE_MEMORY_LIMIT="$PIPELINE_MEMORY_LIMIT"
DASHBOARD_REPLICAS="$DASHBOARD_REPLICAS"
DASHBOARD_CPU_REQUEST="$DASHBOARD_CPU_REQUEST"
DASHBOARD_CPU_LIMIT="$DASHBOARD_CPU_LIMIT"
DASHBOARD_MEMORY_REQUEST="$DASHBOARD_MEMORY_REQUEST"
DASHBOARD_MEMORY_LIMIT="$DASHBOARD_MEMORY_LIMIT"
EOF

# ── Generate manifests ────────────────────────────────────────────────────────
echo ""
echo -e "${CYAN}Generating manifests...${RESET}"
mkdir -p "$OUTPUT"

# Write namespace name for use by Makefile targets.
echo "$NAMESPACE" > "$OUTPUT/.namespace"

for tpl in "$TEMPLATES"/*.yaml; do
  name="$(basename "$tpl")"
  out="$OUTPUT/$name"
  sed \
    -e "s|{{NAMESPACE}}|${NAMESPACE}|g" \
    -e "s|{{IMAGE}}|${IMAGE}|g" \
    -e "s|{{PROJECT_NAME}}|${PROJECT_NAME}|g" \
    -e "s|{{BUYCOTT_DIRECTION}}|${BUYCOTT_DIRECTION}|g" \
    -e "s|{{API_PORT}}|${API_PORT}|g" \
    -e "s|{{DASHBOARD_PORT}}|${DASHBOARD_PORT}|g" \
    -e "s|{{DASHBOARD_SERVICE_TYPE}}|${DASHBOARD_SERVICE_TYPE}|g" \
    -e "s|{{STORAGE_CLASS}}|${STORAGE_CLASS}|g" \
    -e "s|{{ARTIFACTS_SIZE}}|${ARTIFACTS_SIZE}|g" \
    -e "s|{{PIPELINE_CPU_REQUEST}}|${PIPELINE_CPU_REQUEST}|g" \
    -e "s|{{PIPELINE_CPU_LIMIT}}|${PIPELINE_CPU_LIMIT}|g" \
    -e "s|{{PIPELINE_MEMORY_REQUEST}}|${PIPELINE_MEMORY_REQUEST}|g" \
    -e "s|{{PIPELINE_MEMORY_LIMIT}}|${PIPELINE_MEMORY_LIMIT}|g" \
    -e "s|{{DASHBOARD_REPLICAS}}|${DASHBOARD_REPLICAS}|g" \
    -e "s|{{DASHBOARD_CPU_REQUEST}}|${DASHBOARD_CPU_REQUEST}|g" \
    -e "s|{{DASHBOARD_CPU_LIMIT}}|${DASHBOARD_CPU_LIMIT}|g" \
    -e "s|{{DASHBOARD_MEMORY_REQUEST}}|${DASHBOARD_MEMORY_REQUEST}|g" \
    -e "s|{{DASHBOARD_MEMORY_LIMIT}}|${DASHBOARD_MEMORY_LIMIT}|g" \
    -e "s|{{ANTHROPIC_API_KEY}}|${ANTHROPIC_API_KEY}|g" \
    -e "s|{{OPENAI_API_KEY}}|${OPENAI_API_KEY}|g" \
    -e "s|{{GEMINI_API_KEY}}|${GEMINI_API_KEY}|g" \
    "$tpl" > "$out"
  echo -e "  ${GREEN}✓${RESET} $out"
done

# If STORAGE_CLASS is empty, remove the storageClassName field so the cluster
# default storage class is used.
if [[ -z "$STORAGE_CLASS" ]]; then
  sed -i '/storageClassName:/d' "$OUTPUT/04-pvc.yaml"
fi

echo ""
echo -e "${GREEN}${BOLD}Done.${RESET} Manifests written to ${CYAN}k8s/manifests/${RESET}"
echo ""
echo -e "  ${BOLD}make k8s-apply${RESET}   — apply to current kubectl context"
echo -e "  ${BOLD}make k8s-status${RESET}  — check resource status"
echo ""
echo -e "${YELLOW}⚠  k8s/manifests/ contains API keys — do not commit it.${RESET}"
