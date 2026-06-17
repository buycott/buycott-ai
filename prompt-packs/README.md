# Prompt packs — the no-install, interactive approach

The lowest-friction way to participate in the [buycott](../MISSION.md): no
Docker, no Buycott binary, no API key. Just paste a prompt into the coding agent
that ships with your subscription — Claude Code, Codex, or the Gemini CLI — flip
it into auto-approve mode, and let it grind on a large, self-perpetuating task,
nudging it back to work when it pauses, until you hit your plan's rate limit.

Because **you** start an interactive session and the agent does exactly what
it's built to do — agentic coding on a big task — this is ordinary product
usage. It's the hardest variant for a provider to call abuse, while still
putting your flat-rate subscription compute to work. You're not running a
headless bot; you're using the tool, hard.

[`roll.sh`](roll.sh) generates the prompt for you, picking a random project from
a set of ad-lib tables, so every session builds a different real project and no
two runs look the same.

## Use it

1. **Make a throwaway directory.** Auto-approve mode lets the agent run shell
   commands on its own, so give it a sandbox to play in — ideally a disposable
   VM or container, at minimum a fresh empty folder. Never point it at files or
   systems you care about.

   ```bash
   mkdir burn-$(date +%s) && cd "$_"
   ```

2. **Generate a prompt** with the script (from this directory):

   ```bash
   /path/to/prompt-packs/roll.sh -c    # random prompt, copied to your clipboard
   # or: roll.sh            print to stdout
   #     roll.sh -s 1234    reproducible roll (share the seed to coordinate)
   ```

3. **Launch the agent in auto-approve mode** (pick the one you have a sub for)
   and paste the prompt in:

   ```bash
   # Claude Code — auto-accept edits (you still see it work):
   claude --permission-mode acceptEdits
   # …or fully hands-off in a disposable VM/container:
   claude --permission-mode bypassPermissions

   # Codex — never pause for approval, write within the workspace:
   codex --ask-for-approval never --sandbox workspace-write
   # …or fully hands-off in a disposable VM/container:
   codex --dangerously-bypass-approvals-and-sandbox

   # Gemini CLI — auto-approve edits:
   gemini --approval-mode auto_edit
   # …or auto-approve everything:
   gemini --yolo
   ```

4. **Check on it.** It won't run untouched forever — agents periodically decide a
   turn is "done" and yield, especially after a context compaction. That's
   expected, not a failure. When it stops (or whenever you wander back), paste the
   nudge below to send it back to work, or relaunch the session. If it stopped on
   a rate limit, you've done the thing — that's your subscription's subsidized
   capacity, spent; wait for the window to reset and go again.

   ```
   Keep going. Read STATE.md and BACKLOG.md, take the top backlog item, implement
   it with tests, commit, then append at least two new backlog items. Don't stop
   to summarize or ask — continue until I interrupt you.
   ```

   To poke it automatically when it goes idle, see
   [Automating the poke](#automating-the-poke-optional) — but know that crosses
   from "interactive" toward "headless."

## Customize what it builds

The ad-lib options live in the `A`–`E` arrays at the top of [`roll.sh`](roll.sh)
(what to build · theme · language · constraint · how to expand when the backlog
thins). Edit them to taste — add your favorite language, niche the themes, or
swap in project types you'd actually find useful. `roll.sh -h` prints usage.

## Automating the poke (optional)

[`nudge.sh`](nudge.sh) watches the agent's tmux pane and re-sends the keep-going
nudge whenever its output stops changing (i.e., it went idle):

```bash
tmux new -s burn        # then, inside, launch the agent and paste your prompt
./nudge.sh burn         # from another shell: poke pane "burn" when it idles
```

**Heads up:** automating the re-prompt slides this from interactive use toward
the headless/automated category — which is more likely to trip a provider's
automated-use terms. It's here as a convenience for people who've decided to
accept that, not the default. If you want genuinely hands-off autonomy, the
Buycott pipeline is the purpose-built version (with the matching ToS exposure).

## How this compares

This is the **interactive** approach. For a fully autonomous, multi-agent,
parallel version that runs headless and never needs babysitting, use the Buycott
pipeline itself (see the main [README](../README.md)) — at the cost of more
setup and, on a subscription, more exposure to automated-use terms. Trade-offs
are laid out in the marketing notes' "three ways to play it."

## Safety

- Auto-approve modes execute commands without asking. **Run in a disposable
  directory, ideally a throwaway VM or container.**
- Don't run this on a work account, and don't point it at anything you'd be sad
  to lose.
- This is your subscription, your machine, and your electricity. That's the cost
  of the act — weigh it before you start.
