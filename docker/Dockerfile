# Build stage
FROM golang:1.22-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /lumen ./cmd/lumen

# Runtime stage
FROM alpine:3.20

RUN apk add --no-cache ca-certificates git gh curl bash

# Create lumen user
RUN adduser -D -h /home/lumen lumen

COPY --from=builder /lumen /usr/local/bin/lumen

WORKDIR /workspace
USER lumen

ENV LUMEN_HOME=/home/lumen
ENV LUMEN_WORKSPACE=/workspace

ENTRYPOINT ["lumen"]
CMD ["serve"]
