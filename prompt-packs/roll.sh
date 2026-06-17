#!/usr/bin/env bash
#
# Roll the ad-lib tables and print a ready-to-paste marathon-build prompt.
# Edit the A–E arrays below to change what it builds. See README.md.
#
# Usage:
#   ./roll.sh              # random prompt to stdout
#   ./roll.sh -s 42        # reproducible roll (seed)
#   ./roll.sh -c           # also copy to the clipboard (pbcopy/xclip/wl-copy)
#   ./roll.sh | pbcopy     # or pipe it yourself
#
# The chosen combo is printed to stderr; the prompt goes to stdout, so piping
# stays clean.
set -euo pipefail

A=(
  "a command-line tool"
  "a terminal UI (TUI) app"
  "a single-page web app"
  "a REST/GraphQL API service"
  "a small programming language + interpreter"
  "a 2D game"
  "an in-memory database engine"
  "a static-site generator"
  "a developer library / SDK"
  "a cellular-automaton simulation"
  "a parser + compiler for a toy format"
  "a desktop app"
  "a data-visualization dashboard"
  "a chat/bot framework"
  "a build tool / task runner"
  "a roguelike"
  "a spreadsheet engine"
  "a document-format converter"
  "a physics sandbox"
  "a generative-music toy"
)

B=(
  "tabletop RPGs"
  "amateur astronomy"
  "urban gardening"
  "model railroads"
  "competitive cooking"
  "birdwatching"
  "vintage synthesizers"
  "tide pools & marine life"
  "fantasy cartography"
  "coffee roasting"
  "mushroom foraging"
  "retro arcade high-scores"
  "constellation mythology"
  "public-transit nerdery"
  "homebrew beer"
  "paper airplanes"
  "lighthouse keeping"
  "beekeeping"
  "mechanical typewriters"
  "desert ecology"
)

C=(
  "Rust"
  "Go"
  "TypeScript (Node)"
  "Python"
  "Zig"
  "Elixir"
  "Gleam"
  "OCaml"
  "Crystal"
  "modern C"
  "Swift"
  "Kotlin"
)

D=(
  "zero third-party dependencies"
  "a plugin architecture"
  "100% test coverage, TDD-first"
  "fully offline / local-first"
  "a polished TUI"
  "property-based + fuzz tests"
  "an extensive docs site with examples"
  "cross-platform (Linux/macOS/Windows)"
  "a public API designed for extension"
  "accessibility as a first-class concern"
  "internationalization for 5+ locales"
  "a benchmark suite tracking perf over time"
)

E=(
  "adding a plugin system + three example plugins"
  "writing an exhaustive fuzz suite and fixing what it finds"
  "building a docs site with tutorials and a cookbook"
  "adding benchmarks and optimizing the hot paths"
  "porting the core to a second language, kept in sync"
  "adding a TUI/GUI front-end"
  "hardening every edge case and error path"
  "adding CI, releases, and a changelog"
  "adding five new user-facing features"
  "adding i18n and accessibility passes"
  "writing a spec + conformance tests, then passing them"
  "adding import/export for several formats"
)

seed=""
copy=0
usage() { sed -n '3,13p' "$0" | sed 's/^# \{0,1\}//'; }
while getopts ":s:ch" opt; do
  case "$opt" in
    s) seed="$OPTARG" ;;
    c) copy=1 ;;
    h) usage; exit 0 ;;
    *) echo "unknown option: -$OPTARG" >&2; usage >&2; exit 2 ;;
  esac
done

[ -n "$seed" ] && RANDOM="$seed"

a="${A[RANDOM % ${#A[@]}]}"
b="${B[RANDOM % ${#B[@]}]}"
c="${C[RANDOM % ${#C[@]}]}"
d="${D[RANDOM % ${#D[@]}]}"
e="${E[RANDOM % ${#E[@]}]}"

printf '# rolled: %s · for %s · in %s · with %s · expand by %s\n' \
  "$a" "$b" "$c" "$d" "$e" >&2

prompt="$(cat <<EOF
You are running a continuous, open-ended engineering loop. There is no "done" —
this is a living product with an infinite roadmap.

Seed (a starting point you will grow far beyond): build ${a} for ${b}, in ${c},
with ${d}.

Memory — always trust these files over your memory of the conversation, so a
context reset can't make you lose the thread:
- STATE.md   — architecture, what exists, key decisions. Read it at the start of
               every cycle; update it after every task.
- BACKLOG.md — an ordered work list. RULE: every task you finish must append at
               least two new, more ambitious tasks, so the backlog always grows
               faster than you drain it.

Loop — repeat forever, never pausing or asking me anything:
1. Read STATE.md and BACKLOG.md.
2. Take the top item. Implement it for real — working, tested code; run the
   tests; fix failures; commit with a clear message.
3. Append at least two new backlog items it suggests: deeper features, edge
   cases, fuzz tests, refactors, docs, benchmarks, integrations, a second-
   language port.
4. Update STATE.md and PROGRESS.md, then return to step 1.

Rules:
- Never end your turn to ask whether to continue. The answer is always yes.
- If you ever think it's finished or "good enough," that is your cue to expand
  scope (${e}), not to stop.
- Every task must leave the project genuinely better — real code, no busywork,
  no stubs or TODO placeholders.
- Continue until I interrupt you. Begin now: create STATE.md and BACKLOG.md,
  then start the loop.
EOF
)"

printf '%s\n' "$prompt"

if [ "$copy" = 1 ]; then
  if command -v pbcopy >/dev/null 2>&1; then printf '%s' "$prompt" | pbcopy
  elif command -v wl-copy >/dev/null 2>&1; then printf '%s' "$prompt" | wl-copy
  elif command -v xclip >/dev/null 2>&1; then printf '%s' "$prompt" | xclip -selection clipboard
  else echo "(-c: no clipboard tool found — pbcopy/wl-copy/xclip)" >&2; fi
fi
