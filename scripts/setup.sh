#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(dirname "$SCRIPT_DIR")"

echo "==> Checking prerequisites..."

command -v docker >/dev/null 2>&1 || { echo "ERROR: docker is required"; exit 1; }
command -v go    >/dev/null 2>&1 || { echo "ERROR: go is required (for local dev)"; exit 1; }

echo "==> Creating artifacts directory..."
mkdir -p "$ROOT/artifacts"

echo "==> Copying example config..."
if [[ ! -f "$ROOT/config.yaml" ]]; then
    cp "$ROOT/config.example.yaml" "$ROOT/config.yaml"
    echo "    Created config.yaml — set your API keys there or export them as env vars."
else
    echo "    config.yaml already exists, skipping."
fi

echo "==> Building binary..."
cd "$ROOT"
go build -o buycott-local .

echo ""
echo "Setup complete."
echo ""
echo "Next steps:"
echo "  1. Edit config.yaml and fill in your API keys (or export ANTHROPIC_API_KEY etc.)"
echo "  2. Run: docker compose up"
echo "     Or for local dev: ./buycott-local start 'Your product direction here'"
