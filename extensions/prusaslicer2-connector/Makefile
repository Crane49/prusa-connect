# The gates of the PrusaSlicer 2 Connector module. extensions/Makefile runs `check` here,
# and so does `make -C extensions check EXT=prusaslicer2-connector`.

include ../extension.mk

.PHONY: check e2e test

# Lint the Go half, then build both halves, install the release into a demo
# server over HTTP and again over HTTPS, and drive it as PrusaSlicer 2 does.
# See README.md.
check: lint e2e

e2e:
	bash scripts/check.sh

# The Go half alone, in its own folder, for a quick run while editing it.
test:
	go build ./...
	go vet ./...
	go test -count=1 ./...
