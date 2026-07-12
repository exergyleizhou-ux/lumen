# Build stage
FROM golang:1.23-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
ARG COMMIT=unknown
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o /lumen ./cmd/lumen

# Runtime stage
FROM alpine:3.20

RUN apk add --no-cache ca-certificates git github-cli curl bash

# Create lumen user
RUN adduser -D -h /home/lumen lumen

COPY --from=builder /lumen /usr/local/bin/lumen

LABEL org.opencontainers.image.title="Lumen" \
      org.opencontainers.image.description="Lumen hosted Code and Science Lab runtime"

WORKDIR /workspace
USER lumen

ENV LUMEN_HOME=/home/lumen
ENV LUMEN_WORKSPACE=/workspace

ENTRYPOINT ["lumen"]
CMD ["serve"]
