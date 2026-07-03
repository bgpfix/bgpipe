#!/bin/bash

[[ "$1" =~ ^v[0-9]+\.[0-9]+\.[0-9]+[a-z]*$ ]] || { echo "Usage: release.sh VERSION (eg. v0.11.0)" >&2; exit 1; }

VERSION="$1"
MYDIR="$(cd "$(dirname "$0")" && pwd)"
DEST="$MYDIR/../bin/$VERSION"

###############################################

function build()
{
	ARCH="$1"
	SUFFIX="${2:+-$2}"

	echo "Building bgpipe $VERSION for $ARCH"
	CGO_ENABLED=0 GOOS="${ARCH%-*}" GOARCH="${ARCH##*-}" \
		go build \
			-ldflags "-s -w -X main.BuildVersion=${VERSION}" \
			-trimpath -buildvcs=false \
			-o $DEST/bgpipe-${VERSION:1}-${ARCH}${SUFFIX}
}

###############################################

echo "Building in $DEST"
[ -d "$DEST" ] && rm -fr "$DEST"
mkdir -p "$DEST" || { echo "Error: Failed to create directory $DEST" >&2; exit 1; }

# NB: pure Go - anything else can always `go build`
build linux-amd64
build linux-arm64
build darwin-arm64
build darwin-amd64
build freebsd-amd64
build openbsd-amd64
build windows-amd64
build android-arm64
