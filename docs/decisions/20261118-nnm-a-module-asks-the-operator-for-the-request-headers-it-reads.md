---
title: "ADR-20261118-nnm: A module asks the operator for the request headers it reads"
description: "A module lists the request headers of its own protocol in routes.headers, with a reason, and the operator approves each one at install, so a module can answer a client such as PrusaSlicer 2 that says what it wants in a header."
date: 2026-11-18
status: proposed
supersedes: []
amends: [ADR-20260910-74m, ADR-20261028-7be, ADR-20261120-xys]
tags:
  - adr
  - extensions
---
# ADR-20261118-nnm: A module asks the operator for the request headers it reads

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amends [ADR-20261028-7be](20261028-7be-a-module-route-sees-only-the-headers-the-host-passes.md) rule 1 and [ADR-20260910-74m](20260910-74m-a-module-declares-what-it-depends-on.md) with a fourth kind of requirement, and [ADR-20261120-xys](20261120-xys-a-module-route-may-take-no-caller-or-a-large-body-written.md) rule 7 | Builds on [ADR-20261112-2eb](20261112-2eb-prusaslicer-3-reaches-this-server-through-a-compatibility.md)

The host passes a module only a fixed list of request headers. A module that
speaks the protocol of another program needs that program's own headers.
PrusaSlicer 2 sends a print to a printer that runs PrusaLink, and it says
whether to print the uploaded file in a header of its own,
`Print-After-Upload`. A module that answers PrusaSlicer 2 as a PrusaLink
printer cannot tell an upload from an upload and print without it.

This record lets a module ask for such a header, with a reason, and the
operator decide, so a header no module needs never reaches one. It amends
ADR-20261028-7be rule 1: a request reaches a module with the headers every
module gets and the ones the operator granted it. A header still gets the
review 7be asked for. It joins a list in the core before any module may ask
for it. The operator then decides which module gets it. ADR-20261028-7be
rule 3, that each list names what passes, holds.

## rules

### the request headers a module asks for

1. A manifest may list request headers under `routes.headers`, each with a
   `name` and a `reason` of at most 200 characters, and at most 8 headers.
   The reason holds no control or invisible character, so it cannot hide or
   reorder the request lines the operator reads.
   The name is an HTTP token, and the host keeps it in its canonical form,
   such as `Print-After-Upload`.
2. The operator approves each header at install, as a request of its own.
   The install lists it as `header <name> (<reason>)`. An approved install
   writes the name to `service.request_headers`, and the host passes that
   header to the module on every route of the module, staged ones included.
   A header the operator took away stays away on an update, and a header a
   new release adds is a new request. A changed reason is not a new request,
   and the extension page shows the reason of the installed release.

   A header may say `required: true` when the module cannot work without
   it. The install records it as a requirement of the instance. It is a
   fourth kind beside the capability, the core range and the module of
   ADR-20260910-74m. The host
   does not start the instance while `service.request_headers` lacks it. The
   instance's state names the header. An approved update that would leave a
   required header out, because the operator took it away, is refused. The
   install lists it as `header <name>, required (<reason>)`.
3. A module may ask only for a header on the list `grantableHeaders` in
   `sdk/manifest`, today `Print-After-Upload`. The manifest and the
   configuration refuse every other name. A header joins the list by a change
   to the core. That change checks that the header carries no credential and
   names no person, and that no proxy or browser writes it on its own. It
   also checks that the header does not change the method, path or size the
   host authorized. The list only
   grows, so a manifest keeps its meaning on every later core.

   These headers never join the list, and a test holds the list apart from
   them:
   - `Authorization`, `Proxy-Authorization`, `Cookie`, `X-Api-Key`, `DPoP`,
     `X-Setup-Code` and `Host`
   - `Token`, `Fingerprint` and `Code`, a printer's credential on its
     connection and the code it registers with
   - the hop-by-hop headers: `Connection`, `Keep-Alive`, `Proxy-Connection`,
     `TE`, `Trailer`, `Transfer-Encoding` and `Upgrade`
   - `Forwarded`, `Via`, every `X-Forwarded-*`, `X-Real-IP`, `X-Client-IP`,
     `X-Cluster-Client-IP`, `True-Client-IP` and `CF-Connecting-IP`

   The host checks both lists again when it passes a header, so a grant an
   operator wrote into the configuration gets no other header.
4. A header every module gets already, such as `Content-Type`, is refused
   in `routes.headers`, because asking for it changes nothing.
5. A call from another module through `routes.call` carries only the
   headers every module gets.
6. Only request headers are granted. The headers of a module's answer stay
   held to the one list every module gets.

### what the marketplace catalogue lists

7. This rule amends ADR-20261120-xys rule 7. A catalogue release row also
   lists `request_headers`, each with its name and reason, as the
   marketplace reviewed them. An approved marketplace install of a module
   that asks for a header the row does not list is refused with
   `MODULE_REQUESTS_CHANGED`. The host drops a listed header whose name
   rule 3 or 4 refuses, or whose reason breaks rule 1. An install of a
   module that asks for it is then refused too.

## enforcement

| rule | gate |
|---|---|
| 1, 3, 4 | `TestARoutesBlockReadsTheHeadersAModuleAsksFor` in `sdk/manifest` |
| 1, 3, 4 | `TestAHeaderRequestIsRefused` in `sdk/manifest` |
| 3 | `TestEveryDeniedHeaderIsRefusedInEverySpelling` in `sdk/manifest` |
| 3 | `TestEveryGrantableHeaderIsAllowedAndNoneIsDenied` in `sdk/manifest` |
| 3 | `TestACredentialHeaderIsNeverGrantedToAModule` in `internal/platform/auth/session` |
| 3 | `TestAPrintersCredentialHeaderIsNeverGrantedToAModule` in `internal/connector/xbuddy` |
| 3 | `TestTheSetupCodeHeaderIsNeverGrantedToAModule` in `internal/transport/http/httpapi` |
| 2 | `TestAnApprovedInstallGrantsItsRequestHeaders` in `internal/app` |
| 2 | `TestAnInstallWithoutApproveOnlyListsItsRequestHeaders` in `internal/app` |
| 2 | `TestAnUpdateKeepsARequestHeaderTheOperatorTookAwayOut` in `internal/app` |
| 2 | `TestARequiredHeaderIsRead` in `sdk/manifest` |
| 2 | `TestARequiredHeaderIsRecordedAsARequirement` in `internal/app` |
| 2 | `TestAnUpdateThatWouldDropARequiredHeaderIsRefused` in `internal/app` |
| 2 | `TestAnInstanceWithoutARequiredHeaderDoesNotStart` in `internal/app/resourcehost` |
| 2, 3 | `TestAGrantedHeaderReachesTheModuleOnEveryRoute` in `internal/app/resourcehost` |
| 3 | `TestAGrantedRequestHeaderAModuleNeverGetsIsRefusedAtLoad` in `internal/app/config/install` |
| 3 | `TestAGrantedHeaderReachesAModuleAndACredentialNever` in `sdk/routesapi` |
| 7 | `TestARequestHeaderIsHeldToWhatTheOperatorWasShown` in `internal/app` |
| 7 | `TestEveryAskSurvivesEveryHop` in `internal/app` |
| 7 | `TestTheIndexDropsARequestHeaderThatCouldHideWhatItAsks` in `internal/app` |

## rejected

| option | why not |
|---|---|
| keep ADR-20261028-7be's fixed list with no way to ask for more | a module that speaks another program's protocol needs its headers |
| any header except a list of refused names | it fails open, and refusing a name later breaks published manifests |
| a filter on credential words in the name | it refuses real headers such as `Lock-Token` and misses an innocent name |
| `Print-After-Upload` in the list every module gets | every module would get it, and nothing would record which module needs which header |
| a grant of any header the operator names, with no list of refused ones | one hand edit would give a module the person's cookie |

## open items

- The marketplace does not publish `request_headers` yet. Until it does,
  rule 7 refuses an approved install from it of any module with a header.
- An older core refuses a grant of a header a newer core added to the list,
  so a downgrade does not start that instance.
