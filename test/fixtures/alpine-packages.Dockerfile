# Example: Alpine with APK packages
# doner: alpine:3.#
FROM alpine:3.19

# doner: bash:#.#.#, curl:#.#.#, git:ignore
RUN apk add --no-cache \
    bash=5.2.15-r0 \
    curl=8.5.0-r0 \
    git

HEALTHCHECK CMD echo "healthy"
