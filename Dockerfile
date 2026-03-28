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

ENTRYPOINT ["./stallionussy"]
CMD ["serve", "--port", "8080"]
