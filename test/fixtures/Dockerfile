# doner: python:3.13.#-alpine*
FROM python:3.13.11-alpine3.21

# doner: uv:0.#.#
COPY --from=ghcr.io/astral-sh/uv:0.9.24 /uv /uvx /bin/

RUN addgroup -g 1000 workers && adduser -D -u 1000 -G workers workers

# doner: bash:#.#.#, curl:#.#.#, git:ignore
RUN apk add --no-cache bash=5.2.15-r0 curl=8.5.0-r0 git

# doner: uv-build:0.#.#
RUN uv pip install "uv-build==0.9.24"

HEALTHCHECK --interval=30s --timeout=3s \
  CMD python -c "print('ok')"
