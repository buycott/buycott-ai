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

# Optional: to use the `claude-code` provider (run roles on a Claude
# subscription), the `claude` CLI must be on PATH. Uncomment to bundle it:
#   RUN apt-get update && apt-get install -y --no-install-recommends nodejs npm \
#       && npm install -g @anthropic-ai/claude-code \
#       && rm -rf /var/lib/apt/lists/*
# Then authenticate non-interactively by passing CLAUDE_CODE_OAUTH_TOKEN
# (from `claude setup-token`) into the container's environment.

COPY --from=builder /buycott-bin /usr/local/bin/buycott
COPY prompts/ /etc/buycott/prompts/
COPY scripts/entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

VOLUME ["/artifacts"]
ENTRYPOINT ["/entrypoint.sh"]
CMD ["buycott", "--help"]
