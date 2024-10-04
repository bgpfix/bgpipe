#!/bin/bash

[ -z "$1" ] && { echo "Usage: release.sh VERSION" >&1; exit 1; }

VERSION="$1"
DEST="./bin/bgpipe-$VERSION"

###############################################

function build()
{
	ARCH="$1"
	SUFFIX="${2:+-$2}"

	echo "Building bgpipe $VERSION for $ARCH"
	CGO_ENABLED=0 GOOS="${ARCH%-*}" GOARCH="${ARCH##*-}" \
		go build -o $DEST/bgpipe-${ARCH}${SUFFIX}
}

###############################################

echo "Building in $DEST"
rm -fr $DEST
mkdir -p $DEST

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

build darwin-amd64
build darwin-arm64

build freebsd-amd64
build netbsd-amd64
build openbsd-amd64

build windows-amd64
build windows-arm64
