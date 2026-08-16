# Example: Python with pip packages
# doneram: python:3.13.#
FROM python:3.13.0

# doneram: requests:#.#.#, flask:#.#.#
RUN pip install --no-cache-dir \
    requests==2.31.0 \
    flask==3.0.0

HEALTHCHECK CMD python -c "import requests, flask; print('healthy')"
