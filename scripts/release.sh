#!/bin/bash
# Cuts a release: writes MAJOR.MINOR.PATCH to VERSION and the embedded copy,
# commits, and tags vX.Y.Z[-alpha.N|-beta.N]. When sdk/ changed since its last
# tag the sdk takes the same tag, sdk/vX.Y.Z[...]. It pushes nothing.
#
# Usage: ./scripts/release.sh alpha|beta|stable [patch|minor|major]
#        ./scripts/release.sh <version>
#        ./scripts/release.sh --next alpha|beta|stable [patch|minor|major]
#
# --next prints the version the same arguments would release and writes
# nothing. .github/workflows/cut-release.yml asks it first, to file the
# changelog under that version before the release itself.
#
#   alpha   after 0.0.3 → 0.0.4-alpha.1, after 0.0.4-alpha.1 → 0.0.4-alpha.2
#   beta    after 0.0.4-alpha.2 → 0.0.4-beta.1, after 0.0.4-beta.1 → 0.0.4-beta.2,
#           after 0.0.3 → 0.0.4-beta.1
#   stable  after 0.0.4-beta.2 → 0.0.4, after 0.0.3 → 0.0.4
#   0.0.9   exactly that version, for the rare jump the three cannot make
#
# The second word moves a new cycle further than the patch: `alpha minor`
# after 0.0.3 is 0.1.0-alpha.1, `beta major` is 1.0.0-beta.1, `stable minor`
# is 0.1.0. A cycle already at that number continues instead. An alpha or beta is a GitHub prerelease and
# goes to the beta portal, and so does a stable release
# (.github/workflows/beta-portal.yml). The beta portal can also cut a release
# without a laptop: .github/workflows/cut-release.yml runs this script.
#
# It stops before it writes anything when the working tree has uncommitted
# changes, or when docs/CHANGELOG.md is not ready. Before you run it, file the
# fragments in docs/changes/ under a `## <new version> - DATE` heading and
# commit that: scripts/cut-release-changelog.sh <new version> files them and
# deletes them (ADR-20261021-arm).

set -e

PRINT_ONLY=0
if [ "${1:-}" = "--next" ]; then
    PRINT_ONLY=1
    shift
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
VERSION_FILE="${PROJECT_ROOT}/VERSION"
SDK_VERSION_FILE="${PROJECT_ROOT}/sdk/VERSION"
EMBEDDED_VERSION_FILE="${PROJECT_ROOT}/internal/kernel/version/VERSION"

cd "${PROJECT_ROOT}"

for file in "${VERSION_FILE}" "${SDK_VERSION_FILE}"; do
    if [ ! -f "${file}" ]; then
        echo "Error: ${file} not found"
        exit 1
    fi
done

# VERSION holds the release's MAJOR.MINOR.PATCH and nothing else: the npm
# packages, the module ranges and the SDK parse it as a plain triple. The
# prerelease label lives in the tag, and the binaries take their version from
# the tag, so they report 0.0.4-alpha.2 while VERSION says 0.0.4. Where in its
# cycle that version stands is therefore read off the tags.
CURRENT_VERSION=$(tr -d ' \n' < "${VERSION_FILE}")
if [[ "${CURRENT_VERSION}" =~ ^([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
    major=${BASH_REMATCH[1]}
    minor=${BASH_REMATCH[2]}
    patch=${BASH_REMATCH[3]}
elif [[ "${1:-}" =~ ^[0-9] ]]; then
    # A version named outright needs nothing from VERSION; it repairs it.
    major=0; minor=0; patch=0
else
    echo "Error: VERSION holds '${CURRENT_VERSION}', which is not MAJOR.MINOR.PATCH."
    echo "  Name the next version outright: $0 <version>"
    exit 1
fi

# How far a new cycle moves: patch by default, minor or major when asked.
# With a cycle still open, a patch bump continues it, and so does a minor or
# major bump whose number that cycle already is: after 0.1.0-alpha.2,
# `alpha minor` is 0.1.0-alpha.3, not 0.2.0-alpha.1.
BUMP="${2:-patch}"
case "${BUMP}" in
    patch) NEXT_BASE="${major}.${minor}.$((patch + 1))" ;;
    minor) NEXT_BASE="${major}.$((minor + 1)).0" ;;
    major) NEXT_BASE="$((major + 1)).0.0" ;;
    *)
        echo "Error: '${BUMP}' is not patch, minor or major."
        exit 1
        ;;
esac

# The highest N among the tags v<version>-<stage>.N, or 0.
highest() {
    git tag --list "v${CURRENT_VERSION}-$1.*" | sed -E "s/^v${CURRENT_VERSION//./\\.}-$1\.//" |
        grep -E '^[0-9]+$' | sort -n | tail -n 1 | sed 's/^$/0/'
}
STABLE_OUT=0
git rev-parse -q --verify "refs/tags/v${CURRENT_VERSION}" >/dev/null && STABLE_OUT=1
ALPHA=$(highest alpha); ALPHA=${ALPHA:-0}
BETA=$(highest beta); BETA=${BETA:-0}

# A minor or major bump the open cycle does not already stand at starts a new
# cycle, exactly as if the current version were out.
NEW_CYCLE=${STABLE_OUT}
case "${BUMP}" in
    minor) [ "${patch}" = "0" ] || NEW_CYCLE=1 ;;
    major) [ "${minor}" = "0" ] && [ "${patch}" = "0" ] || NEW_CYCLE=1 ;;
esac

KIND="${1:-}"
case "${KIND}" in
    alpha)
        if [ "${NEW_CYCLE}" = "1" ]; then
            NEW_VERSION="${NEXT_BASE}-alpha.1"
        elif [ "${BETA}" -gt 0 ]; then
            echo "Error: ${CURRENT_VERSION} has a beta already; an alpha would go backwards."
            echo "  Cut another beta, or the stable release and then an alpha of the next patch."
            exit 1
        else
            NEW_VERSION="${CURRENT_VERSION}-alpha.$((ALPHA + 1))"
        fi
        ;;
    beta)
        if [ "${NEW_CYCLE}" = "1" ]; then
            NEW_VERSION="${NEXT_BASE}-beta.1"
        else
            NEW_VERSION="${CURRENT_VERSION}-beta.$((BETA + 1))"
        fi
        ;;
    stable)
        if [ "${NEW_CYCLE}" = "1" ]; then
            NEW_VERSION="${NEXT_BASE}"
        else
            NEW_VERSION="${CURRENT_VERSION}"
        fi
        ;;
    "")
        echo "Usage: $0 alpha|beta|stable [patch|minor|major] | <version>"
        exit 1
        ;;
    *)
        if [[ ! "${KIND}" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-(alpha|beta)\.[0-9]+)?$ ]]; then
            echo "Error: '${KIND}' is neither alpha, beta, stable nor a version X.Y.Z[-alpha.N|-beta.N]."
            exit 1
        fi
        NEW_VERSION="${KIND}"
        ;;
esac
BASE_VERSION="${NEW_VERSION%%-*}"

if git rev-parse -q --verify "refs/tags/v${NEW_VERSION}" >/dev/null; then
    echo "Error: the tag v${NEW_VERSION} exists already."
    exit 1
fi
if [ "${PRINT_ONLY}" = "1" ]; then
    echo "${NEW_VERSION}"
    exit 0
fi

echo "=== Release ${NEW_VERSION} ==="
echo "Current version: ${CURRENT_VERSION}"
echo ""

# Check for uncommitted changes
if ! git diff --quiet || ! git diff --cached --quiet; then
    echo "⚠ Warning: You have uncommitted changes. Please commit or stash them first."
    echo "  Run 'git status' to see what needs to be committed."
    exit 1
fi

# sdk/ is a Go module of its own. Go finds the version of a module in a
# subdirectory by a tag with that directory as a prefix, so the sdk tag is
# sdk/vX.Y.Z. A release with no commit under sdk/ since the last sdk tag cuts
# no sdk tag: a new tag over the same code would show authors an upgrade that
# changes nothing.
SDK_CURRENT=$(tr -d ' \n' < "${SDK_VERSION_FILE}")
LAST_SDK_TAG=$(git tag --list 'sdk/v*' --sort=-v:refname | head -n 1)
if [ -z "${LAST_SDK_TAG}" ]; then
    SDK_MOVED=1
elif [ -n "$(git log --oneline "${LAST_SDK_TAG}..HEAD" -- sdk/)" ]; then
    SDK_MOVED=1
else
    SDK_MOVED=0
fi

# Runs before anything is written. It fails when docs/changes/ still holds a
# fragment, or when the newest version heading is not the new version.
sh "${SCRIPT_DIR}/check-release-changelog.sh" "${NEW_VERSION}"

echo "${BASE_VERSION}" > "${VERSION_FILE}"
echo "✓ Updated ${VERSION_FILE} to ${BASE_VERSION}"
mkdir -p "$(dirname "${EMBEDDED_VERSION_FILE}")"
echo "${BASE_VERSION}" > "${EMBEDDED_VERSION_FILE}"
echo "✓ Updated ${EMBEDDED_VERSION_FILE}"
git add "${VERSION_FILE}" "${EMBEDDED_VERSION_FILE}"

# The client packages repeat the core's version, and their gates refuse a
# tree where they disagree with VERSION.
bash "${SCRIPT_DIR}/stamp-version.sh"
git add -u packages examples extensions cmd/new-module

if [ "${SDK_MOVED}" = "1" ]; then
    echo "${BASE_VERSION}" > "${SDK_VERSION_FILE}"
    git add "${SDK_VERSION_FILE}"
    echo "✓ Updated ${SDK_VERSION_FILE} to ${BASE_VERSION}"
else
    echo "○ sdk/ has not moved since ${LAST_SDK_TAG}, so it keeps ${SDK_CURRENT} and takes no tag"
fi

# The commit-msg hook accepts only the prefixes in
# .agents/rules/git-discipline.md, so the subject starts with `chore`.
# The second alpha of a cycle writes the VERSION the first one wrote, so it
# can have nothing to commit; the tag then goes on the commit that is there.
if git diff --cached --quiet; then
    echo "○ VERSION and the packages already say ${BASE_VERSION}; nothing to commit"
else
    git commit -m "chore(release): ${NEW_VERSION}"
    echo "✓ Committed changes"
fi

git tag -a "v${NEW_VERSION}" -m "Release ${NEW_VERSION}"
echo "✓ Created tag v${NEW_VERSION}"

if [ "${SDK_MOVED}" = "1" ]; then
    git tag -a "sdk/v${NEW_VERSION}" -m "sdk ${NEW_VERSION}"
    echo "✓ Created tag sdk/v${NEW_VERSION}"
fi

echo ""
echo "✓ Release ${NEW_VERSION} created successfully!"
echo ""
echo "Next steps:"
echo "  1. Review the changes: git show"
echo "  2. Review the tag: git show v${NEW_VERSION}"
echo "  3. Check that the repository variable PRINTER_MODEL names the printer's model."
echo "     The release workflow builds the printer binary with it and stops without it."
echo "  4. Push when ready: git push && git push origin v${NEW_VERSION}"
echo "     The release workflow hands it to the beta portal as well."
echo ""
