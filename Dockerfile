# StallionUSSY: The Go Brrrr Build
# Multi-stage build for minimal binary size

# Stage 1: Build
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /build

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-w -s -X main.version=docker" \
    -o stallionussy ./cmd/stallionussy/

# Stage 2: Runtime
FROM alpine:3.20

RUN apk add --no-cache ca-certificates tzdata

RUN adduser -D -g '' stallion
USER stallion

WORKDIR /app

COPY --from=builder /build/stallionussy .
COPY --from=builder /build/web ./web

EXPOSE 8080

ENV STALLIONUSSY_PORT=8080
# NOTE: JWT_SECRET must be provided at runtime (via docker run -e, compose env,
# or orchestrator secrets). Do NOT bake secrets into the image.
# The app reads STALLIONUSSY_PORT from the environment automatically when
# --port is not explicitly set on the command line.

ENTRYPOINT ["./stallionussy"]
CMD ["serve"]
