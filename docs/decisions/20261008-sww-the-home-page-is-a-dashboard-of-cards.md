---
title: "ADR-20261008-sww: the home page is a dashboard of cards"
description: "The core keeps dashboards, draws the default one on the home page, and lets a module add cards through the slot dashboard-card."
date: 2026-10-08
status: accepted
amends: [ADR-20260911-4p6]
tags:
  - adr
  - frontend
  - modules
---
# ADR-20261008-sww: the home page is a dashboard of cards

- status: accepted
- date: 2026-10-08

<!-- nav:related -->
> [!NOTE]
> Index: [decisions](README.md) | Supersedes rule 12 of [ADR-20260911-4p6](20260911-4p6-a-module-extends-the-ui-through-one-library-and-four-slots.md) and its two rejected rows for a `dashboard` slot and a settings schema | Adds a slot under [ADR-20260929-nqd](20260929-nqd-the-module-slots-come-from-one-registry.md)

A dashboard is a page a person lays out from cards: printers, the fleet,
temperatures, cameras, notes. The model is Home Assistant's: a card declares
its settings, and the page draws the form. The core stores dashboards and draws one on the home page. A module
adds a card the way it adds a section to a printer's page today: it declares
the card in its manifest and registers a component for it.

## rules

### the record

1. Keep dashboards in the host database, in the table `dashboard`, migrated
   with the other host tables on both engines. A build without the web app
   keeps the table and the routes, because the page is one client of them.
   A configuration may leave dashboards out, as rule 32 says.
2. Give a dashboard a name, a visibility, a grid of `columns` from 4 to 24
   and a row `density`, and a list of cards. It has no description: a line
   under the name took space on every screen and said little. Give each card an
   id unique in its dashboard, a type, a place `x`, `y`, `w`, `h` on the grid,
   a title, and its settings.
3. Keep a card's settings as an object the page wrote, at most 4 KiB written
   out. The core checks the shape of the object and its size, and never the
   fields: the fields belong to the card type.
4. Number a dashboard with an id that is never given out twice, so a link to a
   deleted dashboard never opens another one.
5. Carry a `revision` on every dashboard. A change sends the revision it was
   made against, and one made against an older revision is refused with
   `core.resource/DASHBOARD_CHANGED`.
6. Hold one server to 200 dashboards and one dashboard to 64 cards. A card
   reaches at most row 512, and a tile is at most 32 rows tall.

### who may see and change one

7. Authorize dashboards through the resource kind `dashboard`, of class
   `stored`. The org roles already grant it: a viewer views, an operator
   operates, an administrator configures.
8. Give each dashboard a visibility of `everyone`, `operators` or `admins`. A
   caller sees a dashboard when they hold `view_resource`, `operate_resource`
   or `configure_resource` on the kind, in that order.
9. Filter a listing of dashboards by visibility inside the query, so its count
   never includes a dashboard the caller may not see.
10. Let a caller make, change or delete a dashboard when they hold
    `operate_resource` on the kind and see the dashboard. Never let a change
    set a visibility the caller would not see.
11. Let one dashboard be the default. Marking it takes `configure_resource`,
    and it must be visible to `everyone`.

### the home page

12. Draw the default dashboard on `/`. When the caller sees no default, draw
    the first dashboard they see. When they see none, draw a dashboard the
    page writes from the fleet: a page of that printer for one printer, a farm
    for more. Store nothing for it until a person saves it.
13. Keep the home choice of Settings, Appearance. `dashboard` becomes its
    default, and the choices that open a printer or the printer listing stay.
14. Give every dashboard the address `/dashboards/<id>`. Add `?kiosk=1` to
    draw it over the whole screen for a wall, signed in. The wall view keeps
    the screen awake, hides the pointer when idle, and changes nothing.

### the card a module adds

15. Add the slot `dashboard-card` on the screen `dashboard`, in a new form
    `card`. The slot hands a card its settings and no device.
16. Declare each card under `contributes.cards` in `module.yaml`. An entry
    carries an `id`, a `title`, a `description` and an `icon` from the closed
    list. It also carries a starting `size`, a smallest size `least` in cells
    of a 12-column grid, and a `settings` schema.
17. Name a card type `<namespace>/<id>`. The core's own cards are `core/<id>`
    and are registered through the same path a module uses.
18. Write the settings schema as a list of fields from a closed set: `printer`,
    `printers`, `tag`, `camera`, `choice`, `choices`, `number`, `text`, `note`
    and `switch`. Each field names its `key`, its catalogue `label`, its
    default, and the bounds its kind takes.
19. Draw the settings form from the schema in the core, and check every value
    against it before a card receives it. A value that fails takes the
    default.
20. Let a module register an editor component for a card whose settings a
    schema cannot express. The editor receives the settings and a callback,
    and the core checks what it hands back against the schema.
21. Hand a card component its settings, its size in cells, and whether the
    page is being edited. It reads everything else from the host: the fleet,
    the shared telemetry feed and the cameras. It opens no stream and calls
    no route outside the SDK (ADR-20260911-4p6 rules 20 and 23).
22. Draw each card inside its own error boundary. A card that throws is drawn
    as an error card naming its type, and the rest of the page draws on.
23. Keep a card whose type no installed module declares, and draw it as a
    placeholder that names the type. Saving the dashboard keeps it.

### what it may cost

24. Load the dashboard screen as a route chunk of its own, and its editor as a
    second chunk loaded when a person presses Edit.
25. Load a module's bundle on the home page only when a card of that module is
    on the dashboard drawn.
26. Share one telemetry stream between every chart of a page, with the
    printers it charts as origins. Follow at most 8 printers at once on one
    page, and say so on a chart past that.
27. Read at most one hour of history for a chart, of the paths it charts, and
    thin each line to 600 points.
28. Refresh a camera card no faster than the library's floor for a page of many
    cameras.
29. Add no JavaScript dependency for the grid, the editor or a chart.
30. Keep no copy of the dashboards in the server's memory. A request reads its
    row.

### a change reaches every screen

31. Publish `com.prusa.connect.dashboard.changed` when a dashboard is made,
    changed or deleted, with the origin of the kind `dashboard`. A screen that
    draws it reads it again. A wall screen also reads it every ten minutes,
    for a stream that dropped.

### a deployment may leave dashboards out

32. Switch dashboards with `core.dashboards.enabled`, on by default. Off,
    the build takes the tag `nodashboards` and leaves the dashboard code out,
    and a binary that has the code registers no route for them. `GET
    /api/v1/ui` says `dashboards: false`, and the page then opens on the
    printers and offers no dashboard. The table, the kind, the event and the
    error codes stay in every build, because they are schema and vocabulary.
    The printer's configuration switches dashboards off: it serves one
    printer, and its home page is that printer.

## enforcement

| rule | gate | state |
|---|---|---|
| 1 | `TestSchemaParity` and `TestEveryMigrationIsPinned`, under `make test-engines` | planned |
| 3, 5, 6, 9, 10, 11 | `internal/app/dashboards` tests on both engines | planned |
| 7 | `make resource-kinds-check`, `make authz-vocabulary-check` | planned |
| 15, 16, 18 | `make slots-check`, `go test ./sdk/manifest/` | planned |
| 19, 22, 23 | the dashboard screen's browser tests | planned |
| 24 | the route chunks named in `scripts/route-js.baseline` | planned |
| 26 | the telemetry hub's tests in the library | planned |
| 31 | `make event-types-check` | planned |
| 32 | `make binary-check` on the printer row, and the build tests of `internal/app/config/build` | planned |

## rejected

| option | why not |
|---|---|
| dashboards as a module | a module process costs about 4.4 MB on the printer, and a module cannot read another module's cards |
| a grant on each dashboard | a new object type, actions and a projection, for what three visibility levels give |
| views as tabs inside a dashboard | one grid answers a farm's screen. Tabs can follow as a list of grids |
| an editor component on every card | most cards need a field or two, and a schema keeps every form alike |
| the server checks a card's fields | a new card type would need a server change |
| a public link with a token | a new way to sign in. The owner kept it for later |
| a grid or chart library | earlier research measured 112 kB gzip on every page for a chart |

## open items

| item | what unblocks it |
|---|---|
| per-dashboard grants | an answer to the open question, and then a rule here |
| a public read-only link for a wall screen | an answer to the open question, and then a rule here |
| the summary and clock figures scale to the card, a sixth font size where the craft lane allows five | figures that hold to the type scale on a wall screen |
| an empty card's heading has no stop and its note has one, which the craft lane counts as mixed punctuation | one way to punctuate an empty card |
| conditional cards, shown to some people or at some widths | a rule here and a schema field |
