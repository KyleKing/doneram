# Example: Debian with APT packages
# doner: debian:bookworm-*
FROM debian:bookworm-slim

# doner: wget:ignore, curl:#.#.#
RUN apt-get update && apt-get install -y \
    wget \
    curl \
    ca-certificates && \
    rm -rf /var/lib/apt/lists/*

HEALTHCHECK CMD echo "healthy"
