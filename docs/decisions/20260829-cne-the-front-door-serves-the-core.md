---
title: "ADR-20260829-cne: the front door serves the core"
description: "What the web interface serves at /, where the retired server's application goes, and how one page tells a deployment that authenticates nobody from one that asks who you are."
date: 2026-08-29
status: accepted
tags:
  - adr
  - frontend
  - authentication
---
# ADR-20260829-cne: the front door serves the core

- status: accepted
- date: 2026-08-29

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Builds on [ADR-20260824-dym](20260824-dym-legacy-quarantine.md), [ADR-20260827-wgm](20260827-wgm-a-caller-may-ask-what-it-may-do.md)

`/` served the application generated from the retired server's document. That
application names 49 paths under `/api`, none of them versioned, and reaches
nothing the core answers. The fleet screen the core does feed sat at `/fleet`
and no navigation named it, so the only way to a printer was to type a path.

## rules

1. Serve the core's application at `/`. The core is the product, and the front
   door is the product's.
2. Serve the retired server's application at `/legacy/`, and delete it with the
   server it belongs to.
   It is quarantined under [ADR-20260824-dym](20260824-dym-legacy-quarantine.md), so it is
   reachable and it is never the default.
3. Give the core's application one address. A screen inside it earns a page of
   its own when a URL has to carry its state, and not before.
4. Read `GET /api/v1/auth/info` before anything else and branch on `provider`.
   An empty provider is a deployment that authenticates nobody, and it is
   offered no login.
5. Tell a person they are signed out. A listing refused with 401 swaps the
   screen for the login, and an empty table is never the report.
6. Take a redirect login by sending the browser to `GET /api/v1/auth/login`,
   never by fetching it.
7. Read no session credential from script. The cookie is HttpOnly, and what the
   browser holds is the server's business.

## enforcement

| rule | gate | what it refuses |
|---|---|---|
| 4 | `authenticates` in `frontend/src/clients/core-auth.test.ts` | a screen that offers a login on a server that answers none |
| 4, 6 | `getAuthInfo`, `redirectLoginHref` and `passwordLogin` in the same file | a call built against a path or a method the core does not publish |
| 5 | none. `frontend/src/components/CoreApp.tsx` is held by one browser run | the row is on the old backlog |
| 1, 2, 3 | none. Which page `/` serves is a judgement | review |
| 7 | none. The cookie is HttpOnly, so the browser refuses it and no gate has to | the browser |

## rejected

| option | why not |
|---|---|
| keep `/` as the retired application and put a link on it to the fleet | the link would name the product from inside a page that reaches none of it |
| redirect `/` to `/fleet` | a front door that bounces. The browser then shows two addresses for one screen |
| mount the fleet as a route inside the retired application's router | the two share no client, no store and no login. Joining them ties the core to a document that is being deleted |
| a build-time switch choosing which application `/` serves | the question is which of the two this project is, and a switch answers neither |
| read whether the caller is signed in from `GET /api/v1/auth/info` | it names no account and counts none, on purpose. The refused listing is the only report the core gives |
| fetch `GET /api/v1/auth/login` | it answers 303 to the issuer. Only the browser following it lets the issuer set its own cookies and show its own page |
| keep `/fleet` beside `/` | one screen at two addresses, and nothing has shipped either. Rule 3 |

## open items

- The retired server serves `index.html` at `/`, and that file is now the core's
  front door. Under `serve` it reaches no `/api/v1` and shows a refusal. The fix
  is a redirect to `/legacy/` in the retired server's router, which is a row on
  the old backlog.
- Nothing renders either screen in a test. The reducer that merges a listing with
  the frames that overtook it, and the swap from the fleet to the login, are held
  by browser runs alone. The row is on
  the old backlog.
- The header a signed-in person sees, with the control that ends the session, was
  not loaded in a browser. Reaching it needs a credential this server issued.
