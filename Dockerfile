# Build stage
FROM golang:1.21-alpine AS builder

RUN apk add --no-cache git

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o server ./cmd/server

# Runtime stage
FROM alpine:latest

RUN apk add --no-cache ffmpeg ca-certificates tzdata

WORKDIR /app

# Copy binary and configs
COPY --from=builder /app/server .
COPY --from=builder /app/configs ./configs
COPY --from=builder /app/web ./web

# Create directories
RUN mkdir -p recordings temp hls_output

# Expose ports
EXPOSE 8080 8081 8082

# Set timezone
ENV TZ=Asia/Shanghai

# Run
CMD ["./server", "-config", "configs/config.yaml"]
