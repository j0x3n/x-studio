FROM node:24-bookworm-slim AS frontend
WORKDIR /src
COPY . .
RUN if [ -f frontend/package.json ]; then cd frontend && npm ci && npm run build; fi \
    && mkdir -p /src/web/dist \
    && if [ ! -f /src/web/dist/index.html ]; then cp /src/web/placeholder.html /src/web/dist/index.html; fi

FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /src/web/dist ./web/dist
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /server ./cmd/server

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    chromium fonts-noto-cjk ca-certificates \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --uid 10001 --create-home app
WORKDIR /app
COPY --from=build /server /app/server
COPY templates/ /app/templates/
# prompts is supplied by the front-end owner. Binding the directory also works
# when it was absent at build time, and lets prompts change without recompiling.
RUN mkdir -p /app/data /app/prompts && chown -R app:app /app/data /app/prompts
ENV LISTEN=:8080 DATA_DIR=/app/data TEMPLATE_DIR=/app/templates \
    PROMPT_DIR=/app/prompts CHROME_PATH=/usr/bin/chromium
USER app
EXPOSE 8080
ENTRYPOINT ["/app/server"]
