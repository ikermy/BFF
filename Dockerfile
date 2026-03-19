FROM golang:1.25-alpine AS builder

WORKDIR /app

# Кэшируем зависимости отдельным слоем
COPY go.mod go.sum ./
RUN go mod download

# Копируем исходники и собираем
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /bff ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /worker ./cmd/worker

# ---
FROM alpine:3.19 AS api

RUN apk --no-cache add ca-certificates tzdata wget
WORKDIR /app

COPY --from=builder /bff ./bff
COPY --from=builder /app/configs ./configs

EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- http://localhost:8080/health || exit 1

CMD ["./bff"]

# ---
FROM alpine:3.19 AS worker

RUN apk --no-cache add ca-certificates tzdata
WORKDIR /app

COPY --from=builder /worker ./worker
COPY --from=builder /app/configs ./configs

CMD ["./worker"]

