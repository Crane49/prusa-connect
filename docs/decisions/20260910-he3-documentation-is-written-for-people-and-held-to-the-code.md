---
title: "ADR-20260910-he3: documentation is written for people and held to the code"
description: "A reference page is prose a person writes around a region the generator owns, every operation carries a sample the real router answers, and the site publishes a markdown edition for agents in the same build."
date: 2026-09-10
status: accepted
tags:
  - adr
  - documentation
  - generation
  - api
  - sdk
  - llms
---
# ADR-20260910-he3: documentation is written for people and held to the code

- status: accepted
- date: 2026-09-10

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on [ADR-20260826-swm](20260826-swm-a-generated-reference-is-read-by-a-person.md), [ADR-20260826-53y](20260826-53y-documentation-tiers-and-release-notes.md), [ADR-20260826-shb](20260826-shb-a-document-is-read-at-speed.md)

The site's reference is two generated files, 275 and 714 entries, with no
example on either. The samples exist in Go, in `operationExamples`
(`internal/transport/http/httpapi/examples.go:141`), held by five tests and
shown on no page. The documentation needs hand-written pages, with generated
pages that fill the gaps. A new feature's samples sync on their own. A version
for an LLM matters too, because AI agents read it.

## rules

### a reference page

1. A reference page is written by a person. It says what the thing is for,
   how it works and when to use it, in the register of
   `.agents/rules/writing-style.md`.
2. Its enumerable parts, the signature, the parameters, the samples and the
   errors, sit between two marker comments. The
   generator rewrites that region only, and a `-check` refuses a region that
   differs from the generated one.
3. A page is cut by how it reads. One operation or several share a page, and
   a page that stops reading well is split. No rule fixes the count.
4. A resource page exists for every tag in `apiTags`
   (`internal/transport/http/httpapi/reference.go:63`) and a package page for
   every package under `sdk/`. A tag or package with no page fails the build.
5. A Go identifier's doc comment is its reference text and stays in Go. The
   page is the guide around it.

### samples

6. A sample is a Go value beside the handler, typed as the handler's own
   types. `attachExamples` (`examples.go:60`) writes it into the document, and
   the generator renders it as a curl line, a client call and the response.
7. An operation with no sample is refused by
   `TestEveryPublishedOperationCarriesAnExample` (`examples_test.go:26`). An
   excuse is a named row with a reason in `bodylessOperations`
   (`examplesbodyless.go:9`).
8. Every sample goes through the real router. The table of
   `TestAPublishedExampleIsWhatTheHandlerWrites` (`examples_test.go:118`)
   covers every operation, so a sample that differs from its handler's answer
   fails the build.
9. A path or query parameter carries an example value where it is declared,
   as a struct tag. A gate refuses a parameter without one.
10. The codes an operation returns are declared beside its sample and checked
    against the error registry.

### search

11. A heading is the identifier, unchanged and first (ADR-20260826-swm rule 2), and
    nothing else. The first sentence under it carries the other name a reader
    searches for: the HTTP line, or the receiver.
12. A section holds one operation or one identifier, and every heading has an
    id (`rehypeExplicitHeadingIds`, `docs/astro.config.mjs:116`), so Pagefind
    lands a search on the section.
13. The sidebar (`docs/astro.config.mjs:220`) groups: Overview, Guides, API
    reference, Go SDK, Reference data, Onboarding, Internals, the retired
    server. The two reference groups are generated from their directories.

### the agent edition

14. The site publishes `/docs/llms.txt`, `/docs/llms-full.txt` and a markdown
    twin of every page at its URL with `.md` appended, built from the source
    markdown in the same run as the HTML.
15. A check under `docs-check` (`Makefile:1494`) refuses a page with no twin,
    a twin with no line in the index, and an index line naming nothing.
16. The registries and the OpenAPI document are served as JSON and linked from
    the index, never inlined. `llms.txt` names the OpenAPI document and the
    client SDK as the interfaces an agent uses.
17. The agent edition is public with the site.
18. An MCP server is a later module from the modules repository, and outside
    this record.

### migration

19. The two generated pages become regions inside hand-written pages, and
    `cmd/generate-sdk-reference` becomes the region renderer: `render`
    (`cmd/generate-sdk-reference/main.go:119`) writes one artefact per page.

## what stands from the earlier records

ADR-20260826-swm rules 1 to 12 stand and bind the region. Its rule 13, over every
generated page, narrows to the region, and its open item on the worked example
closes under rule 6. ADR-20260826-53y rule 10 stands. Its rule 6 narrows: a person
writes the page's `audience`, the generator writes only the region. ADR-20260826-shb
stands whole.

## enforcement

| rule | gate | state |
|---|---|---|
| 2 | `-check` per region, on `CompareFile` (`internal/codegen/artefact.go:41`) | owed; per page today |
| 3 | the writing lens in review | no gate |
| 4 | `TestEveryTagHasAResourcePage`, `TestEveryPackageHasAPage` | owed |
| 5 | lint refuses an exported `sdk/` identifier with no comment (`.golangci.yml:422`) | exists |
| 7 | `TestEveryPublishedOperationCarriesAnExample` | exists |
| 8 | `TestAPublishedExampleIsWhatTheHandlerWrites` over every operation | nine operations today |
| 9 | `TestEveryPathParameterCarriesAnExample` | owed |
| 10 | a codes test against the error registry | owed |
| 11 | `TestTwoEntriesCannotShareAnAnchor` (`covered_test.go:371`) | exists |
| 15, 16 | `docs/scripts/check-llms.mjs` from `docs-check` | owed |

## consequences

- the generator renders regions in 39 page files instead of writing two
- `httpapi` gains parameter examples and a codes field on `operationExample`
- every resource and package page owes a person's prose, shipped first with
  the tag description as a baseline that only rises

## rejected

| option | why not |
|---|---|
| one page per operation, always | 93 sidebar rows, and search already lands on the section |
| a fixed count of operations per page | readability decides |
| run the samples against `make demo` in CI | a process and a port to prove what the router table proves |
| a partial the page imports | the vault reader sees a page with a hole |

## what is not decided

- whether a package page's worked example is an `Example` function the
  generator copies
- whether `devices`, at 19 operations, is split at the source into two tags
- whether the `.mdx` index page gets a hand-written twin
