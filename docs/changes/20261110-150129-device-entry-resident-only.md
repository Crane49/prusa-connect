### Added
- [sdk] An entry of `device_card`, `device_section`, `device_tab` or `device_action` in module.yaml may carry `devices: {resident: true}`, and it then shows only on the printer this server runs on. `GET /api/v1/resources` returns the block on the entry.
- [sdk] `SlotOutlet`, `SlotActions` and `moduleTabs()` from `@prusa/connect-ui/slots` take the device record, and show an entry that carries a `devices` block only for a record that meets it. An app built on the library must pass the record, or such an entry shows on no printer.

### Fixed
- A module's menu item runs for the printer or file it was chosen for. Before, when the page showed another printer or file before the module had loaded, the item ran for that one.
