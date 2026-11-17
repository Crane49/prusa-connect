# The gates of the materials extension. extensions/README.md says what an
# extension's Makefile holds.

include ../extension.mk

.PHONY: check e2e

check: lint e2e

e2e:
	sh scripts/check.sh
