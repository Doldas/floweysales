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

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates \
 && rm -rf /var/lib/apt/lists/* \
 && groupadd -g 10001 flowey && useradd -m -u 10001 -g 10001 flowey \
 && mkdir -p /data && chown flowey:flowey /data
WORKDIR /app
COPY --from=go-builder /out/floweysales_backend /app/floweysales_backend
USER flowey:flowey
EXPOSE 8080
ENV REST_API_HOST=0.0.0.0 REST_API_PORT=8080 GOMEMLIMIT=256MiB GOGC=80
CMD ["/app/floweysales_backend"]
