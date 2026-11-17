# The gates of the parts module. extensions/Makefile runs `check` here, and so
# does `make -C extensions check EXT=parts`.

include ../extension.mk

.PHONY: check e2e

# Lint the Go half, then build both halves, install the release into a demo
# server and drive it end to end. See README.md.
check: lint e2e

e2e:
	bash scripts/check.sh
