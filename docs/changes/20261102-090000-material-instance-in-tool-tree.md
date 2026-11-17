### Added
- [sdk] The tool tree carries the material package instance a driver reports for a tool: its uuids, type, colour, full weight and profile temperatures, in `Tool.Reported`.
- [sdk] `connectorapi.ValidColor` says whether a tool's colour is #rrggbb or #rrggbbaa, and `ReadTools` reads no other colour.
- The PrusaLink-compatible xbuddy driver publishes the material package instance a printer reports for a tool, so the tool tree carries it. A spool the printer stops reporting leaves the tool tree.
- [sdk] A module reads whether a device was removed or registered again under a new id, in `deviceapi.Device.Retired`.
- [sdk] A module lists the actions it only asks access.check about under checks: in module.yaml, and the install withholds nothing for them. Materials asks so whether a person may operate a printer. The operator approves each one at install, as `check <action>`.
### Changed
- An install that cannot grant a permission says the module may still ask whether a person holds it.
### Fixed
- A module that publishes events keeps running after its settings change. The rewritten declaration put the namespace in front of each event type a second time, and the module was stopped.
- An approved update keeps the events a module was allowed to publish. It dropped them when the release asked for the same events as before.
