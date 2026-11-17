---
title: "ADR-20261016-qrp: a driver declares the models it reaches"
description: "The core names only the Prusa models its profiles cover. A driver declares the other models it reaches in its dialect, and the host adds them to the served device-model vocabulary at start."
date: 2026-10-16
status: accepted
tags:
  - adr
  - devices
  - vocabularies
  - sdk
---
# ADR-20261016-qrp: a driver declares the models it reaches

- status: accepted
- date: 2026-10-16

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Extends [ADR-20260831-nbj](20260831-nbj-a-module-declares-a-device-kind-and-the-host-places-it.md) rule 22 to the device-model vocabulary | Builds on [ADR-20260825-bhe](20260825-bhe-every-vocabulary-is-a-gated-published-resource.md) rule 4 as [ADR-20260901-b2z](20260901-b2z-driver-declared-permissions.md) rules 5 and 24 amend it, ADR-20260901-b2z rule 11 and [ADR-20260901-7t4](20260901-7t4-build-time-adapter-composition.md) rule 9 | Reads with [ADR-20260828-xdp](20260828-xdp-a-contributed-resource-kind-is-a-grantable-object.md) rule 8, [ADR-20260825-dee](20260825-dee-the-capability-vocabulary.md) rule 18, [ADR-20260827-qvk](20260827-qvk-a-published-version-is-refused-or-it-is-not-published.md)

The device-model vocabulary tells a page what a machine is called and whether
its slots are toolheads. The core knows the Prusa machines on the Buddy
firmware, because `profiles.json` records them. It knows nothing true about a
machine another driver reaches. A row for one in the core names a product in
every build, whether or not the build can reach it. So the driver that
reaches the machine declares its row, and the host places it.

## rules

1. Keep in the core only the models a capability profile covers. The table in
   `internal/printer/profile/models.go` is that set, and `ModelDocs` returns it
   alone.
2. A driver declares every other model it reaches in `connectorapi.Dialect`
   `Models`, when it registers. A row carries the model as the machine spells
   it, a display name, a vendor and the tool changer flag.
3. `ValidateDialect` refuses an empty field, a spelling over `MaxModelBytes`,
   a spelling with no ASCII letter or digit, and two rows of one dialect whose
   spellings fold to one. Folding drops every character that is not an ASCII
   letter or digit and then case, as the profile lookup does for a model
   name. `Register` panics on the refusal, so the author meets it in their own
   build.
4. Gather the declared rows once at start, from the registrations the binary
   carries. The host writes each row's connector from the registration's name,
   so a row cannot name a dialect other than the one that declared it. It
   writes the icon key as `printer-generic`, because the other keys name
   Prusa families and a driver draws its machine with a drawing (rule 10).
5. Refuse the declared row, never the start, when its spelling collides. This
   is ADR-20260901-b2z rule 11 applied to a model. A refused row costs a name on a
   page, and a refused start would stop every printer the build serves.
   - A spelling that names a Prusa machine goes, and the Prusa row stays.
     The profile lookup may resolve it, or it may fold to a core row's model.
     It may also fold to the device type code of a profile the build loads,
     with the dots left out. A page folds a code a device reports the same
     way. That refusal names the code and asks for another spelling.
   - Two drivers' spellings that fold to one both go. The order of `init`
     calls picks no winner.
   - The host logs each refusal as an error at start. The line names the
     spelling, the driver by its qualified name, what it collides with, and
     what to do.
   - The vocabulary serves the same refusals in `refused`, so a test of a
     composed build reads them from the API.
6. Serve both halves from `GET /api/v1/vocabularies/device-models`, as one
   list ordered by spelling. The answer stays public. A driver enters the
   binary only at build time (ADR-20260901-7t4 rule 9), so two deployments of one
   build serve the same rows. That is the reason the comment on
   `vocabularySecurity` gives for the other build-wide vocabularies.
   `device-states` takes an identity because an operator fills it at run
   time, which is not the case here.
7. Generate `docs/vocabularies/device-models.json` and the reference page from
   the core's rows only. The content version and the compatibility gate cover
   that half. A declared row moves no version (ADR-20260827-qvk rule 1) and appears in
   no published artefact, as ADR-20260831-nbj rule 22 settles for a contributed device
   kind.
8. A driver that the build does not carry has no rows and no connector entry.
   The vocabulary lists what this binary can reach.
9. Read the tool changer flag as a hint for drawing a row per tool. It adds no
   capability. A driver with no profiles keeps its whole baseline (ADR-20260825-dee
   rule 18), so a driver that takes a per-tool target declares the `tool`
   argument in its own `Baseline`.


### the drawing

10. A declared model may carry a drawing in `Artwork`, which the driver embeds
    in its own module. `ValidateDialect` refuses a type other than SVG, PNG or
    WebP and a file over `MaxArtworkBytes`. A PNG or a WebP has to begin with
    its type's signature, and an SVG has to hold an `<svg` element somewhere.
    Nothing reads further, so a file that passes can still fail to draw, and
    the web app then draws the machine as unknown.
11. Serve the drawing at `GET /api/v1/vocabularies/device-models/{model}/artwork`,
    public like the row that links it. The answer carries `nosniff` and a policy
    that loads and runs nothing. An SVG is a document, and a browser that opened
    the path alone would run the script inside it.
12. The web app keeps its own drawing and name for every model it has one for.
    It reads a name and a declared drawing from the vocabulary only for a model
    it would otherwise draw as unknown. A driver therefore cannot replace the
    drawing of a Prusa machine. Such a model is most often a declared one, and
    it can be a Prusa model the app has no drawing of.

## enforcement

| rule | gate |
|---|---|
| 1 | `TestEveryProfiledModelIsInTheCatalogue` in `internal/printer/profile` refuses a core row no profile carries |
| 3 | the tests beside `ValidateDialect` in `sdk/connectorapi` |
| 4 | `TestADeclaredModelTakesItsDialectsName` in `internal/app` |
| 5 | the collision tests beside `profile.Place` and in `internal/app`, and `TestARefusedModelIsServedWithItsReason` |
| 6 | `TestADeclaredModelIsServedUnderItsDialect` in `internal/transport/http/httpapi` |
| 7 | `make device-models-check` generates the artefact and the page from `ModelDocs` and fails on a difference |
| 10 | the drawing cases beside `ValidateDialect` |
| 11 | `TestADeclaredDrawingRunsNothing` in `internal/transport/http/httpapi` |
| 12 | the declared-model case in `printer-icon.svelte.test.ts` |
| 2, 6, 11 | `test/compose/modules_test.go` reads the Klipper row and drawing out of a server composed with moonraker |

## rejected

| option | why not |
|---|---|
| a row in the core for each machine another driver reaches | the core then names products in every build, including products a vendor has not announced |
| keep such a row in the core under a code name | a code name names the same machine, in every build |
| keep a refused row in `items` with its reason, as the extension catalogue keeps a refused entry | a page joins a device onto any row by its folded spelling, so a refused row left in `items` would still be drawn |
| keep the dialect's name in the core with no rows, to show it as unavailable | it names the product line again, and no page draws an unavailable dialect |
| serve declared rows only to an identified caller | the rows are a property of the build, so an anonymous reader of one build learns nothing a reader of the binary does not |
| let the tool changer flag add the `tool` argument to the driver's baseline | the driver knows whether it can set a per-tool target, and the core would claim a command the machine may not take |
| refuse the start on a collision | a wrong name on a page would stop every printer in the build. ADR-20260901-b2z rule 11 refuses a declaration instead |
| serve a drawing from the driver's own connector mount | that mount is on the device listener, with no session, and is where a machine calls in |
| first declaration wins on a collision | the order of `init` calls decides which driver a page shows, and nobody chose that order |
