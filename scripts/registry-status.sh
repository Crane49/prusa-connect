#!/bin/sh
# What the local npm registry holds for the two client packages and the
# standards package.
#
#   scripts/registry-status.sh <registry url>
#
# One line per package: every version it holds, and which version each
# dist-tag points at. A name the registry has never been given answers 404,
# which is not a failure here: it is the answer "nothing published yet", and
# this prints that instead of a stack of npm's own error text.
#
# The whole run is one function, called on the last line, so the shell parses
# this file to its end before executing any of it. sh reads a script while it
# runs, and an edit landing mid-run moves the read offset under the reader.
main() {

REGISTRY=$1

if [ -z "$REGISTRY" ]; then
	echo "FAIL  this script takes the registry's URL and was given nothing."
	exit 1
fi

case $REGISTRY in
*/) ;;
*) REGISTRY="$REGISTRY/" ;;
esac

if ! command -v node > /dev/null 2>&1; then
	echo "FAIL  node is not on PATH, and reading the registry needs it."
	exit 1
fi

# node rather than npm: `npm view` writes a paragraph per package and exits
# non-zero on a 404, and what a person wants here is two lines. The registry's
# packument is one GET with no authentication, because the @prusa scope is
# readable by anyone who can reach the port.
node -e '
const registry = process.argv[1];
const names = ["@prusa/connect", "@prusa/connect-ui", "@prusa/frontend-standards"];

const read = async (name) => {
  const url = registry + encodeURIComponent(name);
  const answer = await fetch(url, { headers: { accept: "application/json" } });
  if (answer.status === 404) {
    console.log(name.padEnd(26) + "nothing published");
    return;
  }
  if (!answer.ok) {
    console.error("FAIL  " + registry + " answered " + answer.status + " for " + name);
    process.exitCode = 1;
    return;
  }
  const packument = await answer.json();
  const versions = Object.keys(packument.versions ?? {});
  const tags = Object.entries(packument["dist-tags"] ?? {})
    .map(([tag, version]) => tag + " -> " + version)
    .join(", ");
  console.log(name.padEnd(26) + (versions.length === 0 ? "nothing published" : versions.join(" ")));
  if (tags !== "") console.log("".padEnd(26) + tags);
};

(async () => {
  for (const name of names) await read(name);
})().catch((err) => {
  console.error("FAIL  " + registry + " could not be read: " + err.message);
  process.exit(1);
});
' "$REGISTRY"

}
main "$@"
