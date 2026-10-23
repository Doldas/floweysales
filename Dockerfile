# syntax=docker/dockerfile:1
FROM node:24-bookworm-slim AS frontend
WORKDIR /src
COPY frontend/package.json frontend/package-lock.json ./frontend/
RUN npm ci --prefix frontend
COPY frontend/ ./frontend/
RUN npm run build --prefix frontend

FROM golang:1.27.1-bookworm AS go-builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=frontend /src/www/browser ./www/browser
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/floweysales_backend .

FROM golang:1.27.1-bookworm AS legacy-import-builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/migrate-sqlite ./cmd/migrate-sqlite
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/migrate-sqlite ./cmd/migrate-sqlite

FROM debian:bookworm-slim AS legacy-importer
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
 && rm -rf /var/lib/apt/lists/* \
 && groupadd -g 10001 flowey && useradd -u 10001 -g 10001 --no-create-home flowey
COPY --from=legacy-import-builder /out/migrate-sqlite /app/migrate-sqlite
USER flowey:flowey
ENTRYPOINT ["/app/migrate-sqlite"]

FROM debian:bookworm-slim AS runtime
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
 && rm -rf /var/lib/apt/lists/* \
 && groupadd -g 10001 flowey && useradd -m -u 10001 -g 10001 flowey \
 && mkdir -p /data /secrets && chown flowey:flowey /data /secrets
WORKDIR /app
COPY --from=go-builder /out/floweysales_backend /app/floweysales_backend
USER flowey:flowey
EXPOSE 8080
ENV REST_API_HOST=0.0.0.0 REST_API_PORT=8080 GOMEMLIMIT=256MiB GOGC=80
CMD ["/app/floweysales_backend"]
