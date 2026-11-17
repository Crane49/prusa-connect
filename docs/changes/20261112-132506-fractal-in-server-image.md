### Added
- The server image carries the Fractal module under `/opt/modules/fractal`, so a container can join a Fractal network with no separate build of the module. It needs its settings at install, through `prusa-connect module add -settings`, as [running in a container](../src/content/docs/container.md#bundled-extensions) says.
