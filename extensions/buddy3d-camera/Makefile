# The gates of the Buddy3D camera module. extensions/Makefile runs `check`
# here, and so does `make -C extensions check EXT=buddy3d-camera`. Every target
# builds against the sdk and the packages of this checkout.

include ../extension.mk

ROOT := ../..
NAMESPACE := com.github.aux.prusa3d.buddy3d-camera
CLIENT_FLAGS := -module -document extensions/buddy3d-camera/openapi.json \
	-namespace $(NAMESPACE) -name buddy3d-camera-client -out extensions/buddy3d-camera/client

.PHONY: check test ui-deps ui-check client client-check release

check: lint test client-check ui-deps ui-check

# The Go half alone, in its own folder.
test:
	go build ./...
	go vet ./...
	go test -count=1 -race ./...

# Build the client SDK and the component library, then install client/ and ui/.
ui-deps:
	cd $(ROOT)/packages/typescript && CI=true pnpm install --frozen-lockfile && pnpm run build
	cd $(ROOT)/packages/svelte && CI=true pnpm install --frozen-lockfile && pnpm run build
	cd client && CI=true pnpm install --frozen-lockfile && pnpm run build
	cd ui && CI=true pnpm install --frozen-lockfile

# Lint, type-check and test the browser half.
ui-check:
	cd ui && pnpm run check

# Regenerate the typed client in client/ from openapi.json.
client:
	go -C $(ROOT) run ./cmd/generate-client-sdk $(CLIENT_FLAGS)
	cd client && pnpm run generate:types

# The typed client is what openapi.json generates.
client-check:
	go -C $(ROOT) run ./cmd/generate-client-sdk -check $(CLIENT_FLAGS)

# This platform's release, in bin/release.
release:
	sh scripts/release.sh
