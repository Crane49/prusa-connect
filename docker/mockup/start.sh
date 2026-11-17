#!/bin/sh
# The entry point of every mockup container. The first argument is the name of
# the server: hq, brno, workshop or garage. Everything after it goes to the
# demo.
#
# It installs the Fractal module the way an operator does, with the shipping
# binary's own command, and then starts the server. The module is installed at
# every start and not once, so a container that runs a newer image installs the
# binary in that image and the recorded digest is the digest of what runs.
set -eu

NODE=$1
shift

CONFIG=/etc/prusa-connect/core.yaml
SETTINGS=/etc/prusa-connect/$NODE.json

# The module path is the one the release was built from. The host turns it into
# the namespace of the module, so nothing here writes a namespace.
MODULE=github.com/prusa3d/fractal-link
VERSION=1.0.0

echo "mockup: installing the Fractal module for $NODE"

/prusa_connect module add "$MODULE@$VERSION" \
	-config "$CONFIG" -from /release -approve -approve-listen -settings "$SETTINGS"

echo "mockup: starting $NODE"

exec /prusa_connect_demo -config "$CONFIG" "$@"
