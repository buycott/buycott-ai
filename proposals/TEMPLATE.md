<!--
Copy this to proposals/<short-slug>.md and fill it in. Plain English, no code.
Everything below the title is the prompt the agent will run, so write it for an
engineer who knows this codebase (see CLAUDE.md) but not your intent.
-->

# <short title for the change>

## What to build

<One or two paragraphs in plain English: the feature, fix, or change you want,
and *why*. Describe the outcome, not the implementation.>

## Acceptance criteria

- <concrete, checkable outcome>
- <another>
- Must pass `GOTOOLCHAIN=auto go build ./... && go vet ./... && go test ./...`

## Notes / constraints (optional)

- Relevant files or packages: <e.g. internal/llm, internal/pipeline>
- Don't touch: <anything off-limits>
- Edge cases / gotchas: <...>
