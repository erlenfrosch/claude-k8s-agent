# Stage 1: Go Controller Binary bauen
FROM golang:1.22-bookworm AS controller-build

WORKDIR /src
COPY controller/ .
RUN go build -o /issue-controller ./cmd/main.go

# Stage 2: Agent Runtime
FROM ubuntu:24.04

LABEL org.opencontainers.image.source="https://github.com/erlenfrosch/claude-k8s-agent"
LABEL org.opencontainers.image.description="Autonomer Claude Code Agent fuer Kubernetes"

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update && apt-get install -y --no-install-recommends \
    git curl ca-certificates gnupg jq \
    && rm -rf /var/lib/apt/lists/*

# Node.js 20
RUN curl -fsSL https://deb.nodesource.com/setup_20.x | bash - \
    && apt-get install -y nodejs \
    && rm -rf /var/lib/apt/lists/*

# forgecrate via apt
RUN curl -fsSL https://jmt-labs.github.io/apt/KEY.gpg \
    | gpg --dearmor -o /etc/apt/keyrings/jmt-labs.gpg \
    && echo "deb [signed-by=/etc/apt/keyrings/jmt-labs.gpg] https://jmt-labs.github.io/apt stable main" \
    > /etc/apt/sources.list.d/jmt-labs.list \
    && apt-get update \
    && apt-get install -y forgecrate \
    && rm -rf /var/lib/apt/lists/*

# Claude Code
RUN npm install -g @anthropic-ai/claude-code

# Controller Binary aus Stage 1
COPY --from=controller-build /issue-controller /usr/local/bin/issue-controller

# entrypoint
COPY agent/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh /usr/local/bin/issue-controller

WORKDIR /workspace
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
