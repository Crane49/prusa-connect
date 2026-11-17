---
title: "ADR-20261028-7be: a module route sees only the headers the host passes"
description: "Which request headers reach a module's routes, which answer headers reach the person, and that a call between modules follows the same rules."
date: 2026-10-28
status: accepted
supersedes: []
amends: [ADR-20260827-wmv, ADR-20261027-pq5]
tags:
  - adr
  - extensions
  - security
---
# ADR-20261028-7be: a module route sees only the headers the host passes

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Amends [ADR-20260827-wmv](20260827-wmv-a-module-is-mounted-and-an-adapter-is-constructed.md), [ADR-20261027-pq5](20261027-pq5-a-module-calls-the-routes-of-another-module.md)

The HTTP mount passed every request header to a module, and relayed every answer header except the hop-by-hop ones. A module therefore received the person's session cookie and token. With them it could call the core as that person, outside its own grants. It could also set a cookie, a content security policy, HSTS, CORS and framing headers on the core's origin. A call between modules passed the caller's headers on unchanged. Issue #605 reported it.

A module needs none of those. It learns who is calling from the principal the host sets, which ADR-20260910-cqq rule 6 already requires.

## rules

1. A request reaches a module with the headers `routesapi.RequestHeaders` passes and no other: `Accept`, `Accept-Language`, `Content-Language`, `Content-Type`, `If-Match`, `If-Modified-Since`, `If-None-Match`, `If-Range`, `If-Unmodified-Since`, `Range` and `User-Agent`.
2. An answer reaches the person with the headers `routesapi.ResponseHeaders` passes and no other: `Accept-Ranges`, `Cache-Control`, `Content-Disposition`, `Content-Language`, `Content-Range`, `Content-Type`, `ETag`, `Expires`, `Last-Modified`, `Retry-After`, `Vary` and `X-Content-Type-Options`.
3. Both lists name what passes. A header on neither list stays behind, also one that HTTP adds later, until a change adds it to a list.
4. A call from one module to another passes the headers of rule 1, so a credential one module holds never reaches another.
5. The host applies the lists. The module half of the sdk is code a module may replace, so nothing on the module side is relied on.

## enforcement

`sdk/routesapi/headers_test.go` holds both lists to a request that carries credentials and proxy headers, and to an answer that carries a cookie, the core's security headers and a redirect. `TestAPersonsCredentialsStayWithTheHost` and `TestARelayedCallCarriesNoCredential` in `internal/app/resourcehost` hold the HTTP mount and the call relay to rule 1.

## rejected

| option | why not |
|---|---|
| list the headers that stay behind | a header that HTTP adds later would pass until somebody noticed it |
| let a module ask for more headers in its manifest | no module in this repository needs one, and each would need its own review |
| pass `Location` for redirects | a module could send a person off the server, and no module redirects |
