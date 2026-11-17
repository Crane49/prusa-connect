---
title: The SDK is the contract
description: Extensions compile against one small Go module that imports only the standard library.
audience: build
rewrite:
  describes: current
  implementedBy:
    - sdk
---

`sdk/` is a Go module of its own. Drivers, storages, databases and modules
compile against it and against nothing else in this repository. It imports
only the Go standard library.

Each contract in the SDK comes with a test suite. An implementation that
passes the suite fits the contract.

## Why is it this way?

An author outside the project needs one thing to depend on, and it has to be
small and stable. With no third-party imports, the SDK never forces a
dependency or a version on the code that depends on it.

## What does it mean for my change?

Code under `adapters/` and in a module imports `sdk/` and never `internal/`.
When the core and an adapter need the same type, the type goes into the SDK.

A change to an exported name in the SDK is a change for every author. Add to
a contract where you can. Run the suite of the contract against your
implementation.
