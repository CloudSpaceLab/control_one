FROM mcr.microsoft.com/devcontainers/go:1-1.23-bullseye

# Install observability tooling
RUN apt-get update && apt-get install -y --no-install-recommends \
    curl \
    jq \
    prometheus \
    && rm -rf /var/lib/apt/lists/*

# Install golangci-lint for richer linting in the container
RUN curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | \
    sh -s -- -b /usr/local/bin v1.61.0

ENV GO111MODULE=on
