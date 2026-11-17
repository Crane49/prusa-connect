#!/usr/bin/env bash
# Refuses a credential in the files a commit would carry. `make secrets-check`
# runs it, and CI runs that target on every event.
#
# gitleaks reads every file under the directory it is given, ignored ones
# included, so a local .env or a build under bin/ would fail the run. The scan
# reads a copy of the files git tracks or would add instead, which on a clean
# checkout is the same set CI reads.
#
# .gitleaks.toml holds the rules and the fixture values that pass. A finding is
# printed with the value redacted. To fix one, remove the credential and revoke
# it, or add the exact value to .gitleaks.toml when it is a made-up fixture.
set -euo pipefail

GITLEAKS_VERSION=v8.30.1

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT"

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT INT TERM

# -c lists a tracked file deleted from the working tree too, which tar cannot read.
git ls-files -z -co --exclude-standard |
	while IFS= read -r -d '' f; do [ -f "$f" ] && printf '%s\0' "$f"; done |
	tar --null -T - -cf - | tar -xf - -C "$TMP"

echo "scanning $(find "$TMP" -type f | wc -l | tr -d ' ') files with gitleaks $GITLEAKS_VERSION"
cd "$TMP"
go run "github.com/zricethezav/gitleaks/v8@$GITLEAKS_VERSION" dir . \
	--config .gitleaks.toml --redact --no-banner --verbose
echo "OK    no credential found"
