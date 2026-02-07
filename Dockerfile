FROM golang:1.22-alpine AS builder

WORKDIR /app

# Copy go mod files first for caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary
RUN CGO_ENABLED=0 GOOS=linux go build -o minerhq ./cmd/minerhq

# Final stage
FROM alpine:3.19

RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app

# Copy binary
COPY --from=builder /app/minerhq .

# Copy web assets
COPY --from=builder /app/web ./web

VOLUME /data
EXPOSE 8080

ENV TZ=UTC

CMD ["./minerhq", "-config", "/data/config.json"]
