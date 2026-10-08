FROM golang:1.26-alpine AS builder

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /app/watchdog-agent ./cmd/agent

FROM alpine:3.19

RUN apk add --no-cache git ca-certificates

WORKDIR /app
COPY --from=builder /app/watchdog-agent /app/watchdog-agent
COPY configs/config.yaml /app/configs/config.yaml

RUN addgroup -g 1001 -S appgroup && adduser -u 1001 -S appuser -G appgroup
USER 1001

ENTRYPOINT ["/app/watchdog-agent"]
