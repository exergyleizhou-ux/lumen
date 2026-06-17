FROM golang:1.23-alpine AS builder
WORKDIR /build
COPY . .
RUN go build -o /algo ./cmd/algo

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /algo /algo
COPY testdata/ /testdata/
ENTRYPOINT ["/algo"]
