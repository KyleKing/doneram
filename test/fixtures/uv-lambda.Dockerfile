# Example inspired by https://docs.astral.sh/uv/guides/integration/aws-lambda/
# (using Docker Hub images instead of GHCR/ECR for testing)

# doneram: python:3.13.#
FROM python:3.13.0 AS builder

ENV UV_COMPILE_BYTECODE=1
ENV UV_NO_INSTALLER_METADATA=1
ENV UV_LINK_MODE=copy

WORKDIR /build

COPY requirements.txt .
RUN pip install -r requirements.txt

# doneram: python:3.13.#
FROM python:3.13.0

COPY --from=builder /build /app
COPY ./app /app

WORKDIR /app

CMD ["python", "main.py"]
