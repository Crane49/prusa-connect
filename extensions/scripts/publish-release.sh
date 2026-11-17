#!/bin/sh
# Signs a release that scripts/build-release.sh built, packs it, and uploads
# it to the marketplace. The marketplace then holds it for a person to review.
# Nothing is published to operators before that review.
#
# What it does, in order:
#
#   1. checks that checksums.txt lists every file of the release exactly
#   2. signs checksums.txt with the author's RSA key into checksums.txt.sig,
#      base64 text, and checks the signature with the public half of the key
#   3. packs the release into a .tar.gz the marketplace accepts: regular files
#      only, sorted by path, no directory entries, owner 0, time 0, the
#      binaries mode 755 and every other file 644
#   4. asks GitHub Actions for an OIDC token whose audience is the
#      marketplace, which is how the marketplace knows the upload comes from
#      this repository's release workflow
#   5. posts the archive, the channel and the release notes
#
# --dry-run stops after step 3 and also writes the request body, so the run
# needs no token. A version the marketplace already has is a failure, unless
# --allow-existing is given.
#
# Usage:
#
#   scripts/publish-release.sh --release <dir> --key <private-key.pem>
#       --marketplace <url> --module <go-module-path> --channel stable|beta
#       --changelog <file> --out <archive.tar.gz> [--dry-run] [--allow-existing]
#
# When GITHUB_OUTPUT is set, the script writes `archive`, `status` and `url`
# there for the steps after it.
set -e

usage() {
	echo "usage: $0 --release <dir> --key <private-key.pem> --marketplace <url>" >&2
	echo "         --module <go-module-path> --channel stable|beta --changelog <file>" >&2
	echo "         --out <archive.tar.gz> [--dry-run] [--allow-existing]" >&2
	exit 2
}

fail() {
	echo "FAIL  $*" >&2
	exit 1
}

RELEASE= KEY= MARKETPLACE= MODULE= CHANNEL= CHANGELOG= OUT=
DRY_RUN=0
ALLOW_EXISTING=0

while [ "$#" -gt 0 ]; do
	case $1 in
		--release) RELEASE=$2; shift 2 ;;
		--key) KEY=$2; shift 2 ;;
		--marketplace) MARKETPLACE=$2; shift 2 ;;
		--module) MODULE=$2; shift 2 ;;
		--channel) CHANNEL=$2; shift 2 ;;
		--changelog) CHANGELOG=$2; shift 2 ;;
		--out) OUT=$2; shift 2 ;;
		--dry-run) DRY_RUN=1; shift ;;
		--allow-existing) ALLOW_EXISTING=1; shift ;;
		*) usage ;;
	esac
done

for value in "$RELEASE" "$KEY" "$MARKETPLACE" "$MODULE" "$CHANNEL" "$CHANGELOG" "$OUT"; do
	[ -n "$value" ] || usage
done

case $CHANNEL in
	stable | beta) ;;
	*) fail "--channel is $CHANNEL, and the marketplace knows stable and beta" ;;
esac

# The audience of the token is the marketplace's origin: the scheme and the
# host, with no path and no trailing slash.
MARKETPLACE=${MARKETPLACE%/}
ORIGIN=$(printf '%s\n' "$MARKETPLACE" | sed -n 's|^\(https\{0,1\}://[^/]*\).*|\1|p')
[ -n "$ORIGIN" ] || fail "--marketplace $MARKETPLACE is not an http or https address"

for tool in openssl python3 jq curl; do
	command -v "$tool" > /dev/null 2>&1 || fail "$tool is not on PATH"
done

[ -f "$RELEASE/module.yaml" ] || fail "$RELEASE has no module.yaml"
[ -f "$RELEASE/checksums.txt" ] || fail "$RELEASE has no checksums.txt"
[ -f "$KEY" ] || fail "the key file $KEY does not exist"
[ -f "$CHANGELOG" ] || fail "the changelog file $CHANGELOG does not exist"

NAME=$(sed -n 's/^name:[ 	]*//p' "$RELEASE/module.yaml" | head -1)
VERSION=$(sed -n 's/^version:[ 	]*//p' "$RELEASE/module.yaml" | head -1)

WORK=$(mktemp -d)
trap 'rm -rf "$WORK"' EXIT

# ── 1. The checksums cover the release ───────────────────────────────
# A copy of the release gets a fresh checksums.txt, and the two must match
# byte for byte. A file added or changed after the build fails here.
mkdir "$WORK/copy"
cp -R "$RELEASE"/. "$WORK/copy"/
rm -f "$WORK/copy/checksums.txt.sig"
sh "$(dirname "$0")/write-checksums.sh" "$WORK/copy"
cmp -s "$WORK/copy/checksums.txt" "$RELEASE/checksums.txt" ||
	fail "checksums.txt in $RELEASE does not match its files. Build the release again"

# ── 2. The signature ─────────────────────────────────────────────────
openssl dgst -sha256 -sign "$KEY" -out "$WORK/signature.bin" "$RELEASE/checksums.txt" ||
	fail "openssl could not sign with $KEY"
openssl pkey -in "$KEY" -pubout -out "$WORK/public.pem" 2> /dev/null ||
	fail "openssl could not read the public half of $KEY"
openssl dgst -sha256 -verify "$WORK/public.pem" -signature "$WORK/signature.bin" \
	"$RELEASE/checksums.txt" > /dev/null ||
	fail "the signature does not verify with the public half of $KEY"
{
	openssl base64 -A < "$WORK/signature.bin"
	echo
} > "$RELEASE/checksums.txt.sig"

# ── 3. The archive ───────────────────────────────────────────────────
# Python's tarfile writes the same bytes on Linux and macOS. GNU tar and the
# BSD tar of macOS take different flags, and the BSD one adds AppleDouble
# files unless told not to.
mkdir -p "$(dirname "$OUT")"
python3 -I - "$RELEASE" "$OUT" "$NAME" << 'PYTHON' || fail "the release could not be packed"
import gzip
import io
import os
import re
import stat
import sys
import tarfile

release, out, name = sys.argv[1], sys.argv[2], sys.argv[3]
binary = re.compile(re.escape(name) + r"_[a-z0-9]+_[a-z0-9]+(\.exe)?$")

paths = []
for top, dirs, files in os.walk(release):
    for entry in dirs + files:
        full = os.path.join(top, entry)
        relative = os.path.relpath(full, release).replace(os.sep, "/")
        if entry.startswith("."):
            sys.exit(f"{relative}: a hidden file is not allowed in a release")
        info = os.lstat(full)
        if stat.S_ISLNK(info.st_mode):
            sys.exit(f"{relative}: a link is not allowed in a release")
        if stat.S_ISREG(info.st_mode):
            paths.append(relative)
        elif not stat.S_ISDIR(info.st_mode):
            sys.exit(f"{relative}: only regular files are allowed in a release")

buffer = io.BytesIO()
with tarfile.open(fileobj=buffer, mode="w", format=tarfile.USTAR_FORMAT) as archive:
    for relative in sorted(paths, key=lambda path: path.encode()):
        with open(os.path.join(release, relative), "rb") as handle:
            data = handle.read()
        info = tarfile.TarInfo(relative)
        info.size = len(data)
        info.mode = 0o755 if "/" not in relative and binary.match(relative) else 0o644
        info.mtime = 0
        info.uid = info.gid = 0
        info.uname = info.gname = ""
        archive.addfile(info, io.BytesIO(data))

with open(out, "wb") as handle:
    with gzip.GzipFile(filename="", mode="wb", fileobj=handle, compresslevel=9, mtime=0) as packed:
        packed.write(buffer.getvalue())
PYTHON

SIZE=$(wc -c < "$OUT" | tr -d ' ')
# The marketplace refuses an archive over 100 MB once decoded from base64.
LIMIT=$((100 * 1024 * 1024))
[ "$SIZE" -le "$LIMIT" ] || fail "$OUT is $SIZE bytes, and the marketplace takes at most $LIMIT"

echo "packed $NAME $VERSION into $OUT, $SIZE bytes"

output() {
	if [ -n "${GITHUB_OUTPUT:-}" ]; then
		echo "$1=$2" >> "$GITHUB_OUTPUT"
	fi
}
output archive "$OUT"

# The request body. The archive goes in through a file, because a release of
# tens of megabytes is longer than an argument list may be.
base64 < "$OUT" | tr -d '\n' > "$WORK/archive.b64"
BODY=$WORK/request.json
# The keys are quoted, because `module` is a keyword in jq 1.6.
jq -n --arg mod "$MODULE" --arg channel "$CHANNEL" \
	--rawfile archive "$WORK/archive.b64" --rawfile changelog "$CHANGELOG" \
	'{"module": $mod, "archive": $archive, "channel": $channel, "changelog": $changelog}' > "$BODY"

if [ "$DRY_RUN" = 1 ]; then
	jq '{"module", "channel", "changelog", "archive": "\(.archive | length) characters of base64"}' "$BODY"
	echo "dry run: $NAME $VERSION is signed and packed and was not sent to $MARKETPLACE"
	output status dry-run
	exit 0
fi

# ── 4. The OIDC token ────────────────────────────────────────────────
# GitHub sets these two in a job that has `id-token: write`.
[ -n "${ACTIONS_ID_TOKEN_REQUEST_URL:-}" ] && [ -n "${ACTIONS_ID_TOKEN_REQUEST_TOKEN:-}" ] ||
	fail "no OIDC token can be requested here. Run this in a GitHub job with id-token: write, or pass --dry-run"

# The tokens go to curl in a header file, so they appear in no process list.
printf 'Authorization: bearer %s\n' "$ACTIONS_ID_TOKEN_REQUEST_TOKEN" > "$WORK/request-header"
AUDIENCE=$(jq -rn --arg value "$ORIGIN" '$value | @uri')
TOKEN=$(curl -sS --fail -H @"$WORK/request-header" \
	"$ACTIONS_ID_TOKEN_REQUEST_URL&audience=$AUDIENCE" | jq -r '.value // empty')
[ -n "$TOKEN" ] || fail "GitHub answered no OIDC token for the audience $ORIGIN"
if [ -n "${GITHUB_ACTIONS:-}" ]; then
	echo "::add-mask::$TOKEN"
fi

# ── 5. The upload ────────────────────────────────────────────────────
printf 'Authorization: Bearer %s\nContent-Type: application/json\n' "$TOKEN" > "$WORK/upload-header"
ENDPOINT=$MARKETPLACE/api/publish/v1/connect/releases
STATUS=$(curl -sS -o "$WORK/response.json" -w '%{http_code}' -X POST \
	-H @"$WORK/upload-header" --data-binary @"$BODY" "$ENDPOINT") ||
	fail "the request to $ENDPOINT did not complete"

case $STATUS in
	201)
		jq . "$WORK/response.json"
		URL=$(jq -r '.url // empty' "$WORK/response.json")
		echo "uploaded $NAME $VERSION to the $CHANNEL channel. It waits for review at $URL"
		output status in_review
		output url "$URL"
		;;
	409)
		if [ "$ALLOW_EXISTING" = 1 ]; then
			echo "the marketplace already has $NAME $VERSION, so nothing was uploaded"
			output status exists
			exit 0
		fi
		fail "the marketplace already has $NAME $VERSION. Raise the version in module.yaml and CHANGELOG.md to release again"
		;;
	*)
		MESSAGE=$(jq -r '.error // empty' "$WORK/response.json" 2> /dev/null || true)
		fail "the marketplace answered $STATUS: ${MESSAGE:-$(head -c 500 "$WORK/response.json")}"
		;;
esac
