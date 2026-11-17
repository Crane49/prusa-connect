# What every extension's Makefile includes: the one golangci-lint version the
# repository pins, and a `lint` target over the extension's own Go module.
#
# An extension's Makefile starts with `include ../extension.mk` and then
# defines `check`, which runs everything its gate needs. An extension with no
# Go module sets NO_GO_MODULE = 1 before the include, and `lint` says so and
# passes.

# The version is read from the root Makefile, so the repository pins it in one
# place. This file sits in extensions/, and the root Makefile one level up.
EXTENSION_MK_DIR := $(dir $(lastword $(MAKEFILE_LIST)))
GOLANGCI_VERSION := $(shell sed -n 's/^GOLANGCI_VERSION = //p' "$(EXTENSION_MK_DIR)../Makefile")
ifeq ($(GOLANGCI_VERSION),)
$(error the root Makefile pins no GOLANGCI_VERSION, so extension.mk has no golangci-lint version to install)
endif
GOLANGCI_LINT ?= $(shell go env GOPATH)/bin/golangci-lint

.PHONY: lint

lint:
ifeq ($(NO_GO_MODULE),1)
	@echo "no Go module here, nothing to lint"
else
	@if ! $(GOLANGCI_LINT) version 2>/dev/null | grep -q " $(GOLANGCI_VERSION) "; then \
		echo "Installing golangci-lint v$(GOLANGCI_VERSION)..."; \
		go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v$(GOLANGCI_VERSION); \
	fi
	$(GOLANGCI_LINT) run ./...
endif
