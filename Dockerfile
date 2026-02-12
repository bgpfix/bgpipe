# syntax=docker/dockerfile:1

# multi-stage build: cross-compile on builder platform, ship in scratch
# NB: --platform=$BUILDPLATFORM keeps the Go compiler native (no QEMU needed for build)
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

ARG TARGETOS TARGETARCH TARGETVARIANT

WORKDIR /build

# fetch dependencies first (cached layer)
COPY go.mod go.sum ./
RUN go mod download

# cross-compile static binary
COPY . .
RUN GOARM="$(echo ${TARGETVARIANT} | tr -d v)" \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} GOARM="${GOARM}" \
    go build -ldflags="-s -w" -trimpath -o bgpipe .

# ---

# scratch: zero base overhead; ca-certificates copied from builder
FROM scratch

# ca-certificates: needed for HTTPS URLs (MRT archives, RPKI validators)
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

COPY --from=builder /build/bgpipe /bgpipe

ENTRYPOINT ["/bgpipe"]
CMD ["--help"]
