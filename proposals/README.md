# Proposals — contribute a prompt, not code

This repo is built the way it's meant to be used: **you contribute prompts, an AI writes the code.** Instead of opening a pull request with a diff, you open one with a *prompt* describing what you want built. A maintainer reviews the prompt; once it's approved, an agent (Claude Code) runs it against the repo and opens the resulting code change as its own PR.

## How to submit

1. Copy [`TEMPLATE.md`](TEMPLATE.md) to `proposals/<short-slug>.md` and fill it in — plain English, no code.
2. Open a pull request that adds **only** that one file. (A PR that touches anything outside `proposals/` is closed automatically — see below.)
3. Wait for review. A maintainer may ask you to sharpen the prompt.

## What happens when it's accepted

1. A maintainer adds the **`approved`** label to your PR.
2. `run-proposal.yml` runs your prompt through Claude Code on a clean checkout of the default branch, then runs `go build` / `vet` / `test`.
3. It opens a **new PR** with the generated changes, credits you, archives your prompt under `proposals/`, and links back to your proposal. Your original PR is closed when that result PR merges. If `build`/`vet`/`test` didn't pass, the result PR is opened as a **draft** labeled `build-failing`.
4. **CodeRabbit reviews the result PR automatically**, plus a human. The AI output is **not** trusted blindly.
5. A maintainer merges only once review is clean — or asks for another pass.

So there are gates at every step: a human approves the *prompt*, CI + CodeRabbit + a human review the *generated code*, and nothing merges with unresolved serious findings.

## Review gate on generated PRs

Generated PRs are reviewed by [CodeRabbit](https://coderabbit.ai) (config in `.coderabbit.yaml`, set to `request_changes_workflow` so a "changes requested" stays open until resolved). **Any finding marked major or critical must be resolved before the PR merges** — don't continue past it.

To have the agent fix the feedback instead of patching it by hand, a maintainer comments **`/address`** on the result PR. `address-review.yml` then runs Claude Code on that branch with CodeRabbit's (and any human) review comments as the prompt, pushes the fixes, and asks for re-review. Repeat until clean, then merge.

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
- **Labels:** create `approved` (the run trigger), `ran` (applied to a proposal after a run), `ai-generated` and `build-failing` (applied to result PRs).
- **Branch protection:** keep "require PR review before merge" on the default branch so generated PRs can't self-merge. Require CI + CodeRabbit checks to pass.

### Security model — read this

- **Only maintainers can trigger anything.** `run-proposal.yml` fires on the `approved` *label* (labeling needs write access); `address-review.yml` requires a maintainer `/address` comment. Don't hand out write access casually.
- **No untrusted code from the PR is executed.** The run checks out the default branch (never the PR's head) and only reads the proposal's `.md` text. `validate-proposal.yml` guarantees a proposal PR is exactly one added `proposals/<slug>.md` and nothing else, and the slug is allowlist-validated (`[a-z0-9-]`) before it's used anywhere.
- **Secrets are scoped per step.** `ANTHROPIC_API_KEY` is present only on the agent step; the GitHub token only on the gh/push steps. The `build`/`vet`/`test` step — which runs untrusted, agent-generated code — runs with **no** secrets in scope, and checkouts use `persist-credentials: false` so a token never lingers in `.git/config`.
- **The prompt itself is untrusted input** (prompt-injection). Mitigations: a human reads the prompt before approving; the agent runs in the ephemeral CI sandbox; and its output lands in a PR gated by CI, CodeRabbit, and a second human review before it can merge. Review prompts like you'd review code. The "don't touch `.github/`/CI/secrets" instruction to the agent is a guardrail, not a guarantee — branch protection and review are.
- Result PRs are opened by `GITHUB_TOKEN`, so the standalone `ci.yml` won't re-run on them; `run-proposal.yml` runs `build`/`vet`/`test` itself (and opens a **draft** if they fail). To also get the standalone CI check + let merge-blocking work, open result PRs with a PAT, or require the `run-proposal` run's status.
- **Action versions** are tags (`@v4`) kept current by `.github/dependabot.yml`. For stricter supply-chain hardening, SHA-pin them with a tool like `pinact`/`pin-github-action` (don't pin by hand — a wrong hash is worse than a tag).
