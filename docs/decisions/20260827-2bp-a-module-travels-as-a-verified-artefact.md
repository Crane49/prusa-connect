---
title: "ADR-20260827-2bp: a module travels as a verified artefact"
description: "How a module reaches a machine, what it may declare a dependency on, what an install, an upgrade and a removal do, and why a failed install fails one instance and leaves the host serving."
date: 2026-08-27
status: accepted
tags:
  - adr
  - extensions
  - distribution
  - security
  - operations
---
# ADR-20260827-2bp: a module travels as a verified artefact

- status: accepted, four rules are gated and five are owed
- date: 2026-08-27

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on [ADR-20260827-7jn](20260827-7jn-the-plugin-wire-is-a-specification.md), [ADR-20260827-wmv](20260827-wmv-a-module-is-mounted-and-an-adapter-is-constructed.md) | Reads with [ADR-20260827-rwq](20260827-rwq-a-module-keeps-its-data-out-of-the-host-schema.md) | Proposal decision: X13

A design requirement makes an air-gapped install the normal deployment, so the install
path is a file copy and every other source is a fetcher that ends in one. This
record states what a module may depend on, what is verified before anything
runs, and what a bad install does to the instances beside it.

Earlier research read the incident history of six plugin hosts. A bad install can
stop the whole host in four of them, and the two where it cannot are the two
that keep third-party code out of the serving process.

## rules

1. Install every module into one host-owned directory. A local file, an OCI
   layout and a release asset are three fetchers, and each one ends by writing
   a verified file into it. The air gap is then the ordinary case.
2. Refuse a git reference and a Go module path as install sources. Both need a
   toolchain the customer may not have, and a cold build peaks at 1,480 MiB and
   needs 1.35 GiB of disk before a binary exists.
3. Install no artefact with no recorded checksum, from any source.
   `install.serviceOf` already refuses an absent or malformed one, and a fetcher
   inherits the same bar.
4. Verify the digest before every launch and not at install alone.
   `wirehost.Digest.Verify` does that today, so an artefact replaced on disk
   after installation runs nothing.
5. Carry three signing levels and let the deployment choose which it accepts:
   signed by Prusa, self signed, and unsigned. The customer decides what level
   of security they want.
   - refusing an unsigned artefact is not the default. The level is what the
     operator sets, and each level says what it admits.
   - the levels are ordered. `prusa` admits an artefact this project signed.
     `self-signed` admits that plus one signed by a key the operator trusts.
     `unsigned` admits an artefact with a checksum and no signature.
   - the per-id allowlist survives inside the strictest level, so an operator on
     `prusa` excuses one module id rather than dropping to a weaker level. A
     global switch is set once during a bad afternoon and stays set, which is why
     the escape hatch names what it excuses.
6. Refuse `required: true` on a type the core did not write. A required instance
   that is not serving answers 503 on `/api/v1/readiness`, which empties the pod
   out of its Service and leaves every printer reaching nothing.
7. Declare a module's dependency as a `ref` to an instance, and never as a
   version range on another module. An unsatisfied `ref` is a condition on the
   dependent instance, so one unit degrades and the rest start.
8. Treat an external service as an address in an instance's configuration and
   never as an install precondition. RabbitMQ answering or not answering is
   instance health, and an install that failed because a broker was restarting
   is the wrong failure at the wrong time.
9. Refuse to delete an artefact a live declaration names, and name the
   declaration holding it. Disabling the instance first always works, because a
   disabled instance keeps its configuration and its identity.
10. Fail one instance and keep serving. A bad declaration, a missing artefact, a
    wrong checksum and an unserved contract each fail that instance alone, which
    `resource.Store.reportFailures` and `internal/app/resourcehost/faults.go`
    already do.
11. Carry two install shapes and no more. A process module ships a manifest and
    a per-platform executable; an asset module ships a manifest and an archive
    of files. Every mechanism above the digest is beside the point for the
    second.
12. Write the customer guide by following it on a clean machine, quoting the
    output each step produces. `getting-started.md` earned its quality that way,
    and eight defects appeared that no reading found.

## install, upgrade and remove

| action | the customer does | the host does | on failure |
|---|---|---|---|
| install | copies an artefact in, or points the UI at a file | verifies digest, then signature, then contract major | nothing is installed, and the staging file is deleted |
| declare | names an instance in the UI or the file | validates it, then writes the overlay atomically | 422, and the file on disk is unchanged |
| start | switches the instance on | admission, launch, handshake, `plugin.info` | the instance latches failed with a named reason |
| upgrade | replaces the artefact and its digest | re-verifies on the next launch | the instance stops and the host does not |
| remove | disables the instance, then deletes the artefact | refuses a deletion a live declaration holds | the operator is told which declaration holds it |

Order is the whole of a removal. Deleting an artefact while a declaration names
it is Jenkins' boot failure, quoted in
earlier research.

## the two shapes against the three named modules

| module | shape | what it ships | what is launched |
|---|---|---|---|
| the UI | asset | a manifest and an archive of files | nothing |
| a job planner | process | a manifest and an executable per platform | a supervised child |
| the Connect adapters | asset | a manifest and a route that translates | nothing |

The UI is the cheapest module and it uses none of rules 4, 7 or 10. There is no
launch to verify a digest before, no handshake to negotiate, and no child to
supervise, so its failure is a route answering 404.

The Connect shim sits with the UI. A compatibility layer is a route that
translates and exists so it can be deleted, and giving it a process is more
machinery than the thing it wraps.

## what the digest proves and what it does not

`Digest.Verify` hashes the artefact and compares it against the recorded
`sha256:` before every launch. That catches a file replaced after install, a
partial copy, and an artefact swapped by another process.

It says nothing about who produced the bytes. The digest is recorded in the same
file an operator edits, so a person who downloads a hostile artefact records its
digest and the host then guards it faithfully. Rule 5 is what puts an authority
behind the first use, and it is the half this tree has not built.

That is also what an operator on the `unsigned` level is choosing. The level
is the operator's, so the page that sets it says what the
digest proves and what it does not, in these words.

## enforcement

| rule | gate | state |
|---|---|---|
| 1, 2 | `TestOnlyADirectorySourceInstalls`, over the four refused sources | owed, with the fetchers |
| 3 | `install.serviceOf`'s checksum refusal, in `internal/app/config/install` | exists |
| 4 | `TestAnArtefactThatDoesNotMatchItsDigestIsNotExecuted` | exists |
| 5 | `TestEachSigningLevelAdmitsWhatItDeclares`, and `TestAnUnsignedArtefactNeedsItsOwnAllowlistEntry` beside it | owed. Nothing signs, and the `unsigned` level is what an install runs on until something does |
| 6 | `TestRequiredIsRefusedOnAnImplementationTheCoreDidNotWrite`, and `TestACoreTypeMayBeRequired` beside it | exists |
| 7, 8 | a refusal to add a field. `install.Service` carries a path, a checksum and a contract major | exists by omission |
| 9 | `TestAnArtefactALiveDeclarationNamesIsNotDeleted` | owed. Nothing deletes an artefact yet |
| 10 | the seven cases in `internal/app/resourcehost/service_test.go` | exists |
| 11 | `TestAnAssetModuleNeedsNoHandshake` | owed, with the asset kind |
| 12 | the record of the run, as `getting-started.md` has | none possible |

## rejected

| option | why not |
|---|---|
| a build service, so a customer gets a binary with their module in it | Caddy's model, and an air gap cannot reach it. A design requirement makes the air gap normal |
| pre-building the tag combinations a customer might want | 794,624 binaries after ten optional plugins, measured in earlier research |
| semver ranges between modules | three of nine surveyed systems have them, and none runs a server while it resolves |
| vendoring a second copy of a module, as Helm does with subcharts | a module is a process here, and two processes with one configuration are two instances |
| a global switch that permits unsigned modules | the three levels of rule 5 replace it. A level is a stated posture, and a switch is a boolean somebody flipped |
| one signing posture for every deployment | an air-gapped workshop and a fleet do not want the same bar |
| a review queue before a module reaches an index | HACS is the closest analogue and reviews no code. Planning on a reviewer who does not exist |
| a blocklist of bad modules in our source | it is what a project builds after its boundary failed, and Home Assistant's only grows |
| an install that fails when a broker is unreachable | the broker restarts and the install has to be repeated, for a fault that repairs itself |
| a dependency on a module rather than on an instance | see [the dependency half](#why-a-dependency-is-a-ref) |

### why a dependency is a `ref`

Three of nine surveyed systems solved version conflict between modules, and none
of the three runs a server while it resolves. Helm resolves at package time,
Terraform at plan time and Go at build time. Every host that loads a module into
a live process declares a dependency by name with no version, or refuses the
dependency outright.

A version range is a linking decision, and out of process there is no link. Two
modules that both need events from one sink name the same `sink/rabbit`, so
there is one running instance, no second copy and no diamond. That is systemd's
`Requires=` and Kubernetes' object reference. A service manager is the right
prior art for a print farm, because an unsatisfied dependency degrades one unit
and boots the rest.

## open items

- No fetcher exists, so rules 1 and 2 describe an install path with one source.
  The directory half is what `wirehost.Find` resolves today.
- Nothing verifies a signature, so rule 5 is a design and not a check. It lands
  with the fetchers. Rule 5 names no key custody, so who holds the Prusa key, and how an operator declares a self signed
  one they trust, are tasks on
  the old backlog rather than open choices.
- No uninstall path exists, so rule 9 has nothing to refuse.
- The customer guide of rule 12 needs a module that does work, and ADR-20260827-7jn's
  open items say no contract publishes a method set.
- An asset module has no kind, no mount and no install record, so rule 11's
  second shape cannot be declared. The tasks are on
  the old backlog.
- `install.Service` carries no interpreter, so a module in another language
  needs a native executable per platform. That question is open.
- Whether the artefact belongs to the type or to the instance is open, and
  it changes what a customer downloads.
- The incident evidence behind the survey is documentation and issue threads,
  and Nextcloud's fatal-error case comes from a support thread.
