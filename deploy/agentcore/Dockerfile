# syntax=docker/dockerfile:1.7

FROM --platform=$BUILDPLATFORM golang:1.22-bookworm AS go-builder
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH:-arm64} \
    go build -trimpath -ldflags="-s -w" -o /out/mise .

FROM --platform=linux/arm64 python:3.12-slim-bookworm AS runtime
ENV PYTHONDONTWRITEBYTECODE=1 \
    PYTHONUNBUFFERED=1 \
    MISE_EXECUTABLE=/usr/local/bin/mise \
    MISE_RUNTIME_WORKSPACE_ROOT=/tmp/mise-runtime

WORKDIR /app
COPY agent /app/agent
RUN python -m pip install --no-cache-dir --upgrade pip \
    && python -m pip install --no-cache-dir /app/agent \
    && rm -rf /root/.cache/pip
COPY --from=go-builder /out/mise /usr/local/bin/mise

RUN useradd --create-home --uid 10001 mise \
    && mkdir -p /tmp/mise-runtime \
    && chown -R mise:mise /tmp/mise-runtime /app
USER mise

EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD python -c "import json,urllib.request; assert json.load(urllib.request.urlopen('http://127.0.0.1:8080/ping', timeout=3))['status'] in {'Healthy','HealthyBusy'}"

CMD ["python", "-m", "uvicorn", "mise_agent.runtime_app:app", "--host", "0.0.0.0", "--port", "8080", "--no-access-log"]
