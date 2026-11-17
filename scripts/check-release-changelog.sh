#!/bin/sh
# Checks docs/CHANGELOG.md before a release is tagged. scripts/release.sh runs
# it before it writes anything.
#
#   1. docs/changes/ holds no fragment. scripts/cut-release-changelog.sh files
#      every one under `## X.Y.Z - DATE` and deletes it before the tag is cut
#      (ADR-20261021-arm).
#   2. the newest version heading is the version being tagged.
#
# Usage: check-release-changelog.sh <version> [changelog] [changes-dir]
#
# The two paths are arguments so that internal/arch/release_changelog_test.go
# can run this script over fixtures.
#
# No CI job runs it: CI cuts no tag, and docs/changes/ holds fragments on
# every ordinary push.
set -e

VERSION="${1:-}"
CHANGELOG="${2:-docs/CHANGELOG.md}"
CHANGES="${3:-docs/changes}"

if [ -z "$VERSION" ]; then
	echo "usage: $0 <version> [changelog] [changes-dir]" >&2
	exit 2
fi

if [ ! -f "$CHANGELOG" ]; then
	echo "release: $CHANGELOG is missing. This gate reads it by name and cannot run without it." >&2
	exit 1
fi

if [ ! -d "$CHANGES" ]; then
	echo "release: $CHANGES is missing. This gate reads it by name and cannot run without it." >&2
	exit 1
fi

# Every fragment still waiting. The README says how one is written.
PENDING=$(find "$CHANGES" -maxdepth 1 -type f -name '*.md' ! -name README.md | LC_ALL=C sort)

FAILED=0

if [ -n "$PENDING" ]; then
	COUNT=$(printf '%s\n' "$PENDING" | grep -c .)
	echo "release: $CHANGES still holds $COUNT fragment(s) that no release has filed." >&2
	echo "  Run scripts/cut-release-changelog.sh $VERSION, which files them under" >&2
	echo "  '## $VERSION - $(date -u +%Y-%m-%d)' and deletes them, before the tag is cut." >&2
	echo "  A release that ships its own news as pending leaves the reader nothing to open." >&2
	FAILED=1
fi

if grep -q '^## Unreleased' "$CHANGELOG"; then
	echo "release: $CHANGELOG carries a '## Unreleased' heading. Pending news is a file in $CHANGES." >&2
	FAILED=1
fi

# The newest released heading, which is the first version heading in the file.
# A prerelease label belongs to the version: `## 0.0.1-alpha.1 - DATE` names
# 0.0.1-alpha.1, which is not 0.0.1.
NEWEST=$(grep -E '^## [0-9]+\.[0-9]+\.[0-9]+' "$CHANGELOG" | head -1 | sed -E 's/^## ([0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?).*/\1/')

if [ -z "$NEWEST" ]; then
	echo "release: $CHANGELOG carries no version heading, so nothing names $VERSION." >&2
	echo "  Add '## $VERSION - $(date -u +%Y-%m-%d)' above the entries this release ships." >&2
	FAILED=1
elif [ "$NEWEST" != "$VERSION" ]; then
	echo "release: $CHANGELOG's newest version heading is $NEWEST and the tag is $VERSION." >&2
	echo "  The heading is what a reader opens and the tag is what they installed." >&2
	FAILED=1
fi

if [ "$FAILED" -ne 0 ]; then
	echo "" >&2
	echo "release changelog gates failed. See .agents/rules/changelog-discipline.md" >&2
	exit 1
fi

echo "release: $CHANGELOG names $VERSION and $CHANGES holds no pending entry"
