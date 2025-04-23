FROM golang:1.21.5-bullseye AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY . .
RUN go build -v -o apo-receiver ./cmd

FROM debian:bullseye-slim AS runner
RUN apt-get update && \
    apt-get install -y ca-certificates && \
    # 清理缓存以减小镜像大小
    rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY receiver-config.yml /app/
COPY sqlscript /app/sqlscript/
COPY --from=builder /build/apo-receiver /app/
CMD ["/app/apo-receiver", "--config=/app/receiver-config.yml"]