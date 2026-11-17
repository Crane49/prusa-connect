---
title: Design decisions
description: The decisions that shape this project, each on one short page.
audience: build
rewrite:
  describes: current
  implementedBy:
    - internal
    - sdk
---

These pages hold the decisions that shape the code. Each one says what was
decided, why, and what it means for a change you want to make.

Read them before a larger contribution. A change that goes against one of
them needs a reason, and the place to give it is the pull request.

| decision | in one sentence |
|---|---|
| [The core knows devices, not printers](the-core-knows-devices.md) | no brand, model or file format in the core |
| [Features are modules](features-are-modules.md) | what goes beyond the basics is added from outside the core |
| [The SDK is the contract](the-sdk-is-the-contract.md) | extensions compile against one small module |
| [A driver declares what its device can do](a-driver-declares.md) | the core reads declarations and never guesses |
| [One registry for each kind of name](one-registry.md) | error codes, events, permissions and settings are registered once |
| [Permissions are checked everywhere](permissions.md) | the same answer on every way into the server |
| [Events have one shape](events.md) | every event travels in the same envelope |
| [The core keeps no history](no-history-in-the-core.md) | a provider stores history when one is installed |
| [The API stays compatible](api-compatibility.md) | a breaking change is caught before it merges |
| [A module asks, and the operator approves](modules-ask.md) | permissions, ports and data are granted at install |
