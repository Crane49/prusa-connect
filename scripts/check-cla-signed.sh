#!/usr/bin/env bash
# Fails when an author of a pull request's commits has not signed CLA.md, so
# the pipelines of an unsigned pull request stop at their first job instead of
# running for nothing.
#
# The signatures are the ones .github/workflows/cla.yml records in
# signatures/cla.json on the branch cla-signatures. That workflow is the check
# a merge waits for. This script only spends no runner time before it passes,
# and .github/workflows/cla.yml starts the failed runs again once everybody has
# signed.
#
# Usage: GH_TOKEN=... check-cla-signed.sh <owner/repo> <pull request number>
#
# An author GitHub links to no account cannot be matched to a signature, so it
# fails too. Bots sign nothing, as in cla.yml.
set -euo pipefail

REPO="${1:?usage: $0 <owner/repo> <pull request number>}"
PR="${2:?usage: $0 <owner/repo> <pull request number>}"

# No file yet means nobody has signed. Any other failure to read it stops the
# check, because a check that cannot read the list must not pass.
err=$(mktemp)
trap 'rm -f "$err"' EXIT
if ! signed_json=$(gh api "repos/$REPO/contents/signatures/cla.json?ref=cla-signatures" --jq .content 2>"$err"); then
	if grep -q "Not Found" "$err"; then
		signed_json=""
	else
		cat "$err" >&2
		echo "::error::could not read signatures/cla.json on the branch cla-signatures"
		exit 1
	fi
fi
signed=""
if [ -n "$signed_json" ]; then
	signed=$(printf '%s' "$signed_json" | base64 -d | jq -r '.signedContributors[].name')
fi

authors=$(gh api --paginate "repos/$REPO/pulls/$PR/commits" \
	--jq '.[] | .author.login // ("an unlinked author, " + .commit.author.email)' | sort -u)

missing=()
while IFS= read -r author; do
	[ -n "$author" ] || continue
	case "$author" in
	*"[bot]") continue ;;
	esac
	if ! grep -qxF "$author" <<<"$signed"; then
		missing+=("$author")
	fi
done <<<"$authors"

if [ "${#missing[@]}" -gt 0 ]; then
	for author in "${missing[@]}"; do
		echo "::error::$author has not signed the contributor licence agreement"
	done
	echo "Sign it in the pull request with the comment the CLA check asks for, see CONTRIBUTING.md." >&2
	echo "The pipelines start again once everybody has signed." >&2
	exit 1
fi
echo "every author of pull request #$PR has signed the contributor licence agreement"
