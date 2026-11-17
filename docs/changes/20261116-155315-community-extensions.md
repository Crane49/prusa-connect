### Changed
- The Extensions page lists only the official marketplace extensions until an organization administrator switches on Show community extensions in the marketplace settings. Installing or updating an unverified extension asks the person to accept the risk first, and every row shows its trust badge.
### Added
- `PUT /api/v1/modules/marketplace` takes `community`, and the marketplace object it and `GET /api/v1/modules/index` answer carries it. It says whether an organization administrator chose to list the community extensions, those whose trust is not `official`, and it is false until one does.
