# syntax=docker/dockerfile:1.7

FROM golang:1.25-alpine AS builder
WORKDIR /src
RUN apk add --no-cache git
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/control-plane ./cmd

FROM golang:1.25-alpine AS goose-builder
RUN go install github.com/pressly/goose/v3/cmd/goose@v3.22.1

FROM alpine:3.20 AS migrate
RUN apk add --no-cache ca-certificates
COPY --from=goose-builder /go/bin/goose /usr/local/bin/goose
COPY migrations /migrations
WORKDIR /migrations
ENTRYPOINT ["goose", "-dir", "/migrations"]

FROM gcr.io/distroless/static-debian12:nonroot AS runtime
WORKDIR /app
COPY --from=builder /out/control-plane /app/control-plane
ENV CONFIG_PATH=/app/config.yaml
USER nonroot:nonroot
EXPOSE 8081 50051
ENTRYPOINT ["/app/control-plane"]