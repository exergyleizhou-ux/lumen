# SpaceX-style: build artifact outside, ship minimal container.
# No compiler in the runtime image. No build tools. Just the binary + data.
FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY algo /algo
COPY testdata/ /testdata/
ENTRYPOINT ["/algo"]
