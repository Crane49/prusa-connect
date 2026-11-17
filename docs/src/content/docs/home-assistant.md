---
title: Home Assistant
description: Show your printers in Home Assistant with the Prusa Connect (on-premise) integration from HACS, next to a server you run or with the server inside Home Assistant.
audience: run
rewrite:
  describes: current
  implementedBy:
    - internal/app/config/discovery.go
    - internal/transport/http/httpapi/apppairings.go
---

Home Assistant shows your printers through the **Prusa Connect (on-premise)**
integration. It lives in its own repository,
[prusa3d/prusa-connect-homeassistant](https://github.com/prusa3d/prusa-connect-homeassistant),
and you install it with HACS, the community store for Home Assistant. The
integration uses the same public API and event stream as any other app. The
server needs no setting for it.

## Where the server runs

The integration works in one of two ways. You choose when you add it.

| way | what runs where | pick it when |
|---|---|---|
| inside Home Assistant | Home Assistant downloads the server from the GitHub releases, checks its signature, runs it and installs its updates | you want one machine to run everything |
| with a server of its own | the server runs on a Raspberry Pi, a NAS or in Docker, set up as [Install from a release](./install-from-a-release.md) shows | the server already runs, or other people and apps use it too |

## Install the integration

1. In Home Assistant, open HACS, then the menu with three dots, then
   **Custom repositories**.
1. Add `https://github.com/prusa3d/prusa-connect-homeassistant` with the
   category **Integration**.
1. Install **Prusa Connect (on-premise)** and restart Home Assistant.
1. Open **Settings > Devices & services > Add integration** and pick
   **Prusa Connect (on-premise)**.

## Connect Home Assistant to the server

Home Assistant finds a server on the same network without an address typed
in. The server announces itself over mDNS as `_prusa-connect._tcp`, as
[Discovery on the local network](./config/discovery.md) explains. You can also
type the server's address with its port.

Home Assistant then pairs with the server. It shows a short code and a link to
`/pair` on your server. You open the link, sign in, check the code and approve
Home Assistant. The token the server hands out never passes through your
clipboard. [Scoped credentials and app pairing](./scoped-credentials-and-app-pairing.md)
explains the flow.

Home Assistant asks for the `operate` scope. With it, the buttons can pause,
resume and stop a print. If you approve only `read`, the sensors work and
Home Assistant cannot send commands.

## What you get in Home Assistant

- a device for each printer, with its state, print progress, temperatures and
  buttons to pause, resume and stop a print
- the web app in the Home Assistant sidebar, under **Prusa Connect**
- updates over the event stream as they happen, with no polling

The integration's README lists every entity and option.
