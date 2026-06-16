FROM golang:1.22-bookworm AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN GOTOOLCHAIN=auto go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOTOOLCHAIN=auto go build -o /buycott-bin ./

FROM debian:bookworm
RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates \
        docker.io \
    && rm -rf /var/lib/apt/lists/*

# Optional: the CLI/subscription providers (claude-code, codex, gemini-cli) each
# need their CLI on PATH. They're Node packages — uncomment the ones you use:
#   RUN apt-get update && apt-get install -y --no-install-recommends nodejs npm \
#       && npm install -g @anthropic-ai/claude-code @openai/codex @google/gemini-cli \
#       && rm -rf /var/lib/apt/lists/*
# Each authenticates via its own login (subscription), not an API key:
#   claude-code — pass CLAUDE_CODE_OAUTH_TOKEN (from `claude setup-token`)
#   codex       — run `codex login` (credentials persist under ~/.codex)
#   gemini-cli  — run `gemini` once to log in (credentials persist under ~/.gemini)
# Persist the relevant home dirs (volume mount) so logins survive restarts.

COPY --from=builder /buycott-bin /usr/local/bin/buycott
COPY prompts/ /etc/buycott/prompts/
COPY scripts/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

VOLUME ["/artifacts"]
ENTRYPOINT ["/entrypoint.sh"]
CMD ["buycott", "--help"]
