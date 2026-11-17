#!/bin/sh
# Builds the full binary for another processor inside Docker, and writes it to
# bin/prusa_connect_<arch>. `_build-cross-full` in the Makefile calls it
# with the architecture, the Docker platform for it, the Go version from go.mod
# and the version to stamp into the binary.
#
# Needs Docker with buildx. It stops when an argument is missing or Docker is
# not on PATH. `make build-cross ARCH=<arch> VARIANT=minimal` builds without
# Docker.
#
# Usage: sh scripts/build-cross-full.sh <arch> <docker platform> <go version> [build version]
set -e

ARCH=$1
PLATFORM=$2
GO_VERSION=$3
BUILD_VERSION=${4:-}

if [ -z "$ARCH" ] || [ -z "$PLATFORM" ] || [ -z "$GO_VERSION" ]; then
	echo "FAIL  Usage: sh scripts/build-cross-full.sh <arch> <docker platform> <go version>"
	exit 2
fi

if ! command -v docker >/dev/null 2>&1; then
	echo "Error: Docker is required for the full cross-build. It runs in .github/dockerfiles/Dockerfile.build."
	echo "Hint: 'make build-cross ARCH=$ARCH VARIANT=minimal' avoids Docker."
	exit 1
fi

docker buildx create --name prusa-connect-builder --use 2>/dev/null ||
	docker buildx use prusa-connect-builder 2>/dev/null || true

echo "Building full binary for $ARCH (platform $PLATFORM)..."
docker buildx build \
	--platform "$PLATFORM" \
	--file .github/dockerfiles/Dockerfile.build \
	--build-arg TARGETOS=linux \
	--build-arg GO_VERSION="$GO_VERSION" \
	--build-arg BUILD_VERSION="$BUILD_VERSION" \
	--target binary \
	--output "type=local,dest=./bin/tmp_$ARCH" \
	.

# buildx can exit 0 without writing the binary, so the file is checked.
if [ ! -f "bin/tmp_$ARCH/prusa_connect" ]; then
	echo "FAIL  Error: full binary was not produced. Check Docker buildx output"
	exit 1
fi

mv "bin/tmp_$ARCH/prusa_connect" "bin/prusa_connect_$ARCH"
chmod +x "bin/prusa_connect_$ARCH"
rm -rf "bin/tmp_$ARCH"
echo "OK    bin/prusa_connect_$ARCH"
