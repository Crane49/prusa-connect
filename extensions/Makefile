# The gates of the first-party extensions. They stay out of the root Makefile
# and the core's CI workflow; .github/workflows/extensions.yml runs them.
#
# Every folder here holding a module.yaml is an extension, found by a walk, so
# adding one needs no edit to this file. `shape` holds each one to the same
# layout first, then `check` runs `make check` in each.
#
#   make -C extensions check            every extension
#   make -C extensions check EXT=parts  one extension
#   make -C extensions lint             golangci-lint over each one
#   make -C extensions shape            the layout alone

FOUND := $(patsubst %/,%,$(dir $(wildcard */module.yaml)))
EXTENSIONS := $(if $(EXT),$(EXT),$(FOUND))

.PHONY: check lint shape

check: shape
	@set -e; for ext in $(EXTENSIONS); do \
		echo "== $$ext"; "$(MAKE)" -C $$ext check; \
	done

lint:
	@set -e; for ext in $(EXTENSIONS); do \
		echo "== $$ext"; "$(MAKE)" -C $$ext lint; \
	done

shape:
	@bash scripts/check-shape.sh $(EXTENSIONS)
