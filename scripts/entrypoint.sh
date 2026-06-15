#!/usr/bin/env bash
set -euo pipefail

# Ensure artifacts dir and state subdir exist.
mkdir -p /artifacts/.buycott

exec "$@"
