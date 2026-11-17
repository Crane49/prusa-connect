---
paths:
  - "**/package.json"
  - "**/pnpm-lock.yaml"
  - "**/pnpm-workspace.yaml"
  - "**/.npmrc"
  - "go.mod"
  - "go.sum"
  - "**/Dockerfile"
  - "docker-compose*.yml"
  - ".github/workflows/*.yml"
  - ".github/dependabot.yml"
---

# Supply chain

What a dependency, an image or a CI action may be, and how it gets into this
tree. The JavaScript packages are `packages/typescript`, its three consumers
under `packages/typescript/testdata/consumers/`, `packages/svelte`, `frontend/`
and `docs/`. The Go modules are the root and `sdk/`.

## A dependency enters through the manifest and the lockfile together

The manifest is the file that names what a package wants (`package.json`,
`go.mod`). The lockfile records the exact version that was resolved
(`pnpm-lock.yaml`, `go.sum`).

1. Edit the manifest, refresh the lockfile, commit both in one `chore:` commit
2. Never run the package manager's `add` command in a tracked package

| ecosystem | install from the manifest | never |
|---|---|---|
| pnpm | `pnpm install --frozen-lockfile` | `pnpm add <pkg>` |
| go | `go mod tidy` after editing `go.mod` | `go get <pkg>@<version>` |

The one `pnpm add` in this tree is `scripts/check-client-sdk-install.sh`, which
installs the package it has just published into a throwaway copy of a consumer.
Nothing it writes is committed.

## Versions are exact and three days old

pnpm 11 reads only auth and registry settings from `.npmrc`. Every other
setting lives in the package's `pnpm-workspace.yaml`, so that file carries the
policy and `.npmrc` carries a registry scope at most.

| setting | value | why |
|---|---|---|
| `savePrefix` | `''` | a written version is exact, never `^` or `~` |
| `minimumReleaseAge` | `4320` | a version must have been public for three days before pnpm resolves it |
| `allowBuilds` | the dependencies that need a build script, or `{}` | pnpm 11 refuses every dependency script the map does not name |
| `verifyDepsBeforeRun` | `error` | a script does not run against a `node_modules` that drifted from the lockfile |

3. Every setting in the table, in every JavaScript package. `{}` for `allowBuilds` when
   nothing needs a script, so the choice is visible
4. A dependency that needs a build script is a named `true` in `allowBuilds`
   with a comment saying which binary it builds. `docs/` names `esbuild` and
   `sharp`
5. `minimumReleaseAge` equals the `cooldown` in `.github/dependabot.yml`. Move
   one and the other moves in the same commit, or Dependabot proposes a version
   pnpm refuses
6. A consumer that installs a package this tree publishes seconds earlier lists
   it in `minimumReleaseAgeExclude`, and nothing else
7. A vulnerability fix that cannot wait three days runs the install with
   `--config.minimumReleaseAge=0` once and says so in the commit body
8. A package with other projects beneath it sets `packages: ['.']`. pnpm's
   default is every `package.json` below, and `packages/typescript` would
   otherwise fold its consumers in

## Images and CI actions are pinned

Pinned means the reference names one exact build, so the same input always
gives the same result.

9. A container image is `name:tag@sha256:<hex>`. `docker-services.md` holds
   the rule and the fixtures that follow it, and it is not repeated here
10. A GitHub action is `uses: org/action@<full commit sha> # vX.Y.Z`

## Publishing uses a short-lived credential

11. The npm release publishes through a Trusted Publisher, the OIDC flow, in
    which npm accepts a signed token GitHub issues to the workflow. It holds
    no npm token
12. A long-lived token that cannot be avoided is a repository secret scoped to
    one environment, and a workflow reads it from there

## Rejected

| option | why not |
|---|---|
| the policy in `.npmrc` | pnpm 11 ignores every non-auth setting there, so the file would say one thing and the install do another |
| `ignoreScripts: true` | it silences the project's own lifecycle scripts too, and `allowBuilds` already refuses every dependency script by default |
| a floating `latest` tag or a caret range | a build that cannot be reproduced cannot be rolled back |

## Enforcement

| gate | holds |
|---|---|
| `pnpm install --frozen-lockfile` | refuses a lockfile that disagrees with the manifest, and reports the release-age check it ran |
| `verifyDepsBeforeRun: error` | refuses `pnpm run` against a drifted `node_modules` |
| owed | a check that every `pnpm-workspace.yaml` carries the settings of rule 3 and that rule 5's numbers agree |
| `internal/arch/workflow_pins_test.go` | every `uses:` in `.github/workflows/` carries a full commit sha and its tag in a comment |

The owed settings check has a natural home: one step in `make client-sdk-check`
for `packages/typescript` and its consumers, one in `make frontend-check` for
`frontend/` and `docs/`.
