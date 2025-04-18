# syntax=docker/dockerfile:1

# Build stage
FROM golang:1.23-alpine AS builder
WORKDIR /app

# Install build dependencies
RUN apk add --no-cache gcc musl-dev

# Download dependencies first (better caching)
COPY go.mod go.sum ./
RUN go mod download

# Copy all Go source files
COPY *.go ./
COPY static ./static

# Build the application
RUN CGO_ENABLED=0 GOOS=linux go build -o random-joke .

# Final stage
FROM alpine:3.19
WORKDIR /app

# Copy only the built binary and static files
COPY --from=builder /app/random-joke .
COPY --from=builder /app/static ./static

EXPOSE 8888
CMD ["./random-joke", "-port=8888"]
