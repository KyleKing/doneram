# Example: Debian with APT packages
# doneram: debian:bookworm-*
FROM debian:bookworm-slim

# doneram: wget:ignore, curl:#.#.#
RUN apt-get update && apt-get install -y \
    wget \
    curl \
    ca-certificates && \
    rm -rf /var/lib/apt/lists/*

HEALTHCHECK CMD echo "healthy"
