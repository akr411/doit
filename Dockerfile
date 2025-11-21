# Build stage
FROM golang:1.25-alpine AS builder

# Install build dependencies for CGO
RUN apk add --no-cache gcc musl-dev

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build with CGO enabled (required for bbolt)
RUN CGO_ENABLED=1 go build -o doit cmd/main.go

# Final stage
FROM alpine:latest

LABEL org.opencontainers.image.source=https://github.com/akr411/doit

# Install runtime dependencies
RUN apk --no-cache add ca-certificates libc6-compat

WORKDIR /root/

# Copy the binary from builder
COPY --from=builder /app/doit .

ENTRYPOINT ["./doit"]
