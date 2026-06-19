# =============================================================================
# Stage 1: Build the Go binary
# =============================================================================
FROM golang:1.25-alpine AS builder

WORKDIR /build

# Cache dependency downloads
COPY go.mod go.sum ./
RUN go mod download

# Copy source and build
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o /build/dynamodb-sage .

# =============================================================================
# Stage 2: Minimal runtime image
# =============================================================================
FROM alpine:3.21

RUN apk add --no-cache ca-certificates wget

WORKDIR /app

# Copy the compiled binary
COPY --from=builder /build/dynamodb-sage /app/dynamodb-sage

# Copy the config file
COPY config.yaml /app/config.yaml
COPY config/kafka.yaml /app/config/kafka.yaml

# Create the data directory for the SQLite audit DB
RUN mkdir -p /app/data

# Default environment variables
ENV MCP_TRANSPORT_MODE=http
ENV CONFIG_PATH=/app/config.yaml
ENV DYNAMO_SAGE_DB=/app/data/audit.db
ENV DYNAMO_SAGE_ADDR=:8080

EXPOSE 8080

ENTRYPOINT ["/app/dynamodb-sage"]
