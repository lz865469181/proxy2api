# syntax=docker/dockerfile:1

FROM golang:1.25-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/proxy2api ./cmd/proxy2api

FROM alpine:3.21
WORKDIR /app

RUN addgroup -S app && adduser -S -G app app

COPY --from=builder /out/proxy2api /app/proxy2api
COPY config/config.example.yaml /app/config/config.example.yaml

RUN mkdir -p /app/data /app/config && chown -R app:app /app

USER app
EXPOSE 8080

ENTRYPOINT ["/app/proxy2api"]
CMD ["-config", "/app/config/config.yaml"]

