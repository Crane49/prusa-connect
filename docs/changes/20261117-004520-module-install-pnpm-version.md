### Fixed
- The `install.sh` that `make new-module` writes runs pnpm from inside each project, so corepack uses the pinned pnpm version. It failed with `ERR_PNPM_BAD_PM_VERSION` when corepack defaulted to another major version.
