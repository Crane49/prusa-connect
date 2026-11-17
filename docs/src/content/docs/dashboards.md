---
title: Dashboards
description: Make the home page your own. Arrange cards of printers, prints, cameras, charts and notes, share a dashboard with your team and put it on a wall screen.
audience: run
rewrite:
  describes: current
  implementedBy:
    - internal/dashboard
    - internal/platform/dashboards
    - internal/app/dashboards.go
    - internal/transport/http/httpapi/dashboards.go
    - packages/svelte/src/shell/dashboard
    - frontend/src/lib/screens/dashboard.svelte
---

A dashboard is a page of cards on a grid. Each card shows one thing: a
printer, many printers, the prints that end next, a camera, a temperature
chart, a clock or a note. The server keeps every dashboard, so the people of
your organisation see the same dashboards on every device.

The home page is a dashboard.

## What the home page shows

The home page shows the first of these that exists:

1. the default dashboard, which an administrator chooses
2. the first dashboard you may see, by name
3. a dashboard named **Overview** that the server writes from your printers:
   one printer large with its camera, or the whole fleet when you have more
   than one printer

The written dashboard is not stored. It is stored the first time somebody
presses **Edit** on it.

If you prefer a page of printers, choose it in **Settings, Appearance, Home
page**. The **Dashboards** entry in the navigation still opens the dashboards.

## Make and change a dashboard

1. Open the list of dashboards from the name at the top of the page, and
   press **New dashboard**.
2. Give it a name, and pick the template it starts from: empty, a print farm,
   one printer, or a wall screen.
3. The dashboard opens in the editor. Press **Add a card** to add a card. It
   goes to the first free place, and its settings open at once.
4. Drag a card by its grip to move it, and drag its corner to resize it. The
   other cards make room. With the keyboard, the arrow keys move the card that
   has focus, and Shift with the arrow keys resizes it.
5. Press **Save**. **Undo** and **Redo** work until then, and Ctrl+Z, Ctrl+Y and
   Ctrl+S do the same.

The **Grid** menu of the editor sets the number of columns, the space between
cards and who sees the dashboard.

A card of one printer asks which printer it shows. A card of many printers
shows the printers you choose in its settings, or every printer when you
choose none. Its filters, such as a tag or a state, narrow either set.

Two people can change the same dashboard. The second save is refused, and the
page offers to load the saved dashboard. Nothing is overwritten without a
person seeing it.

## Who sees a dashboard

Each dashboard has a visibility. It follows the roles of your organisation.

| Visibility | Who sees it |
|---|---|
| Everybody | every person of the organisation |
| Operators and administrators | operators and administrators |
| Administrators only | administrators |

To make, change or delete a dashboard, you need the operator role. Only an
administrator may make a dashboard visible to administrators only, and only an
administrator chooses the default. The default dashboard is always one that
everybody sees.

A person who may not see a dashboard gets the same answer as for a dashboard
that does not exist.

## Share a dashboard and show it on a wall

**Wall view** shows the dashboard over the whole screen of this device.
**Share** gives two addresses. The first opens the dashboard. The second opens
it over the whole screen, with nothing that changes it. A person who opens
either address must be signed in, and must be allowed to see the dashboard.

For a screen on a wall:

1. Make an account with the viewer role for the screen.
2. Sign the screen in once with that account.
3. Open the wall screen address.

The wall view keeps the screen on, and hides the pointer and its two buttons
after a few seconds. A change saved on another device shows on the wall as
soon as it is saved. Press Escape to leave the wall view. A card of many
printers that has more printers than fit shows how many more there are under
the list, because nobody scrolls a wall.

## Cards from modules

A module can add its own cards. They appear in **Add a card** under the
module's name, beside the cards of the core. The core draws the settings of a
module's card from the card's schema and checks them, as for its own cards.
[Writing a service module](./writing-a-module.md) describes how a module
declares a card.

A card of a module that is no longer installed stays on the dashboard, as a
card that says so. Install the module again and the card works again.

## Switch dashboards off

Dashboards are on by default. To leave them out, write this in the
configuration:

```yaml
core:
  dashboards:
    enabled: false
```

A build made from that configuration leaves the dashboard code out, so the
binary is smaller. The home page then opens on the printers, and the
**Dashboards** entry and the home choice are not shown. The configuration a
printer installs switches dashboards off.

## Limits

The server keeps the limits below, so a dashboard stays light enough for a
printer to serve.

| Limit | Value |
|---|---|
| Dashboards | 200 |
| Cards on one dashboard | 64 |
| Settings of one card | 4 KiB |
| Printers whose temperatures one page charts | 8 |
| Temperature history on a chart | 1 hour |
