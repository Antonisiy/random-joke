# syntax=docker/dockerfile:1
FROM golang:1.23 AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o random-joke main.go

FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/random-joke .
COPY --from=builder /app/static ./static
EXPOSE 8888
CMD ["./random-joke", "-port=8888"]
