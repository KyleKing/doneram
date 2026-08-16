# Simple Python example for testing basic functionality

# doneram: python:3.13.#
FROM python:3.13.11

# doneram: ignore
COPY --from=ghcr.io/astral-sh/uv:0.9.24 /uv /uvx /bin/

WORKDIR /app

COPY requirements.txt .
RUN uv pip install --system -r requirements.txt

COPY . .

CMD ["python", "app.py"]
