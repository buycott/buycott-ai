#!/usr/bin/env bash
#
# Optional: auto-poke an interactive agent that has gone idle, by sending the
# "keep going" nudge into its tmux pane when its output stops changing.
#
# !! This automates the interaction. A periodic, unattended re-prompt slides you
# !! from "interactive use" toward the headless/automated category — which is
# !! more likely to trip a provider's automated-use terms. Use a personal
# !! account you're willing to risk, or just paste the nudge by hand.
#
# Setup — run the agent inside tmux:
#   tmux new -s burn
#   (inside) claude --permission-mode acceptEdits     # paste your roll.sh prompt
# Then, from another shell:
#   ./nudge.sh burn          # watch pane "burn"; poke when idle (90s poll)
#   ./nudge.sh burn 30       # poll every 30s
#
set -uo pipefail

usage() {
  sed -n '2,18p' "$0" | sed 's/^#\{1,2\} \{0,1\}//'
}

target="${1:-}"
interval="${2:-90}"

[ -z "$target" ] && { usage >&2; exit 2; }
command -v tmux >/dev/null 2>&1 || { echo "nudge: tmux is required" >&2; exit 1; }
tmux list-panes -t "$target" >/dev/null 2>&1 || {
  echo "nudge: no tmux session/window/pane '$target'" >&2; exit 1; }

NUDGE="Keep going. Read STATE.md and BACKLOG.md, take the top backlog item, implement it with tests, commit, then append at least two new backlog items. Don't stop to summarize or ask — continue until I interrupt you."

echo "nudge: watching '$target'; poking when idle (poll ${interval}s). Ctrl-C to stop." >&2
prev=""
while true; do
  sleep "$interval"
  cur="$(tmux capture-pane -p -t "$target" 2>/dev/null || true)"
  if [ -n "$prev" ] && [ "$cur" = "$prev" ]; then
    tmux send-keys -t "$target" "$NUDGE" Enter
    echo "nudge: idle → poked at $(date +%H:%M:%S)" >&2
    prev=""   # force a fresh capture next cycle; don't double-poke
  else
    prev="$cur"
  fi
done
