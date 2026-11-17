### Fixed
- An update, online or from an offline bundle, is no longer refused because the release's module wire contract changed, such as from 1.0.0 to 1.1.0. The server being updated runs this check, so 0.0.1-alpha.7 still refuses 0.0.1-alpha.8, and its binary has to be replaced manually.
- An online update that fails verification, or whose migration rehearsal fails, now ends with the reason on the Updates page. Stage another update right away, without cancelling the failed one first.
- A server now reads the version report of a release that adds fields to it, where it refused that release before.
- An update whose verification or migration rehearsal was stopped, by a closed page or a server restart, now continues from that step at the next Run preflight and prepare.
