# A Fractal network of three nodes

This folder runs a small Prusa Fractal network in one process and prints what
happens. Three nodes: `hq` at the top, `hq/brno` in the middle, and
`hq/brno/printer-1` at the bottom. The printer owns one interface and `hq`
keeps a copy of its state.

```
go run .
```

The program prints four lines:

1. the copy `hq` has after the printer reported its state
2. the copy after the nozzle heated up
3. the result of a method `hq` called on the printer
4. the copy after the printer accepted the new setpoint

## What the code shows

The interface is defined in `printer.json` and not in Go. The program reads
the file at run time. It has two properties and one method. To add a property,
edit that file.

The nodes send messages over the in-memory transport in
`sdk/fractal/fractaltest`, which keeps every link in one process. A deployment
uses the NATS transport in `packages/fractal/natsbind`. The rest of `main.go`
stays the same, because a node uses a transport only through the `Binding`
interface in `sdk/fractal/node`.

There is no network, no broker and no configuration file. To run it you need Go
on your machine.

## Where the rules are

The protocol is defined by `fractal.md`, written and owned by the Fractal
team. The packages under `sdk/fractal` cite the section each rule comes from.
