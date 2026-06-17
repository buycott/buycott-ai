# Proposals — contribute a prompt, not code

This repo is built the way it's meant to be used: **you contribute prompts, an AI writes the code.** Instead of opening a pull request with a diff, you open one with a *prompt* describing what you want built. A maintainer reviews the prompt; once it's approved, an agent (Claude Code) runs it against the repo and opens the resulting code change as its own PR.

## How to submit

1. Copy [`TEMPLATE.md`](TEMPLATE.md) to `proposals/<short-slug>.md` and fill it in — plain English, no code.
2. Open a pull request that adds **only** that one file. (A PR that touches anything outside `proposals/` is closed automatically — see below.)
3. Wait for review. A maintainer may ask you to sharpen the prompt.

## What happens when it's accepted

1. A maintainer adds the **`approved`** label to your PR.
2. `run-proposal.yml` runs your prompt through Claude Code on a clean checkout of the default branch, then runs `go build` / `vet` / `test`.
3. It opens a **new PR** with the generated changes, credits you, archives your prompt under `proposals/`, and links back to your proposal. Your original PR is closed when that result PR merges.
4. A human reviews and merges the result PR (or asks for another pass). The AI output is **not** trusted blindly — the result PR is the review gate.

So there are two gates: a human approves the *prompt*, and a human approves the *generated code*.

## Writing a good prompt

- Be specific about the **outcome** and the **acceptance criteria**, not the implementation.
- Point at relevant files / packages if you know them (`internal/llm`, `CLAUDE.md` has the map).
- Call out constraints: "don't add dependencies," "must pass `go test ./...`," edge cases.
- Keep it to one focused change. Sprawling "rewrite everything" prompts produce slop and get rejected.

---

## Maintainer setup (one-time)

The runner needs an Anthropic credential and two labels.

- **Secret:** add `ANTHROPIC_API_KEY` (repo → Settings → Secrets and variables → Actions). Alternatively a `CLAUDE_CODE_OAUTH_TOKEN` from `claude setup-token` — swap the env var in `run-proposal.yml`.
- **Optional variable:** `PROPOSAL_MODEL` (e.g. `claude-opus-4-8`) to pin the model; defaults to Claude Code's default.
- **Labels:** create `approved` (the run trigger) and `ran` (applied after a run).
- **Branch protection:** keep "require PR review before merge" on the default branch so generated PRs can't self-merge.

### Security model — read this

- **Only maintainers can trigger a run.** `run-proposal.yml` fires on the `approved` *label*, and adding a label requires write access. Don't hand out write access casually.
- **No untrusted code is executed from the PR.** The run checks out the default branch (never the PR's head) and only reads the proposal's `.md` text. The validation workflow guarantees a proposal PR contains nothing but that file.
- **The prompt itself is untrusted input** (prompt-injection / "make the agent do something dumb"). Mitigations: a human reads the prompt before approving, the agent runs in the ephemeral CI sandbox with only `ANTHROPIC_API_KEY` + a scoped `GITHUB_TOKEN`, and its output lands in a PR for a second human review before it can merge. Review prompts like you'd review code. The agent is told not to touch `.github/`, CI, or secrets — but treat that as a guardrail, not a guarantee.
- Generated result PRs are opened by `GITHUB_TOKEN`, so the standalone `ci.yml` won't re-run on them; `run-proposal.yml` runs `build`/`vet`/`test` itself and reports the result in the PR body. To get full CI on result PRs too, open them with a PAT instead.
