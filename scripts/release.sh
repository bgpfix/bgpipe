#!/bin/bash

[[ "$1" =~ ^v[0-9]+\.[0-9]+\.[0-9]+[a-z]*$ ]] || { echo "Usage: release.sh VERSION (eg. v0.11.0)" >&1; exit 1; }

VERSION="$1"
MYDIR="$(cd "$(dirname "$0")" && pwd)"
DEST="$(realpath $MYDIR/../bin/$VERSION)"

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
rm -fr $DEST
mkdir -p $DEST

build darwin-arm64
build darwin-amd64
build linux-amd64
build linux-arm
# GOARM=6 build linux-arm 6
# GOARM=7 build linux-arm 7
build linux-arm64
build linux-mips
build linux-mips64
build linux-mips64le
build linux-ppc64
build linux-ppc64le

build freebsd-amd64
build netbsd-amd64
build openbsd-amd64

build windows-amd64
build windows-arm64
