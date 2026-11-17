# Example configuration profiles

This directory contains ready-made configuration profiles for common engine and
listener combinations.

Every key in every profile is a key this build reads. A key it does not read is
refused when the file is loaded, naming the key and the line it is written on.
So a profile cannot configure a subsystem that is not there.

## Profiles

- `config.core-only.yaml`
  The core on its own, with no `resources` block: no storage is opened, no
  history store is created and no measurement is written.

- `config.sqlite.yaml`
  The SQLite engine, which is what the shipped configuration runs on.

- `config.postgres.yaml`
  The PostgreSQL engine.

- `config.tls.yaml`
  TLS-enabled browser/API listener and TLS-enabled printer-facing listener.

- `config.printer.yaml`
  The server on a printer's own board, with a small database pool, a memory
  limit and no module. It goes with the binary `make build-printer` writes.

## Usage

Inspect inferred build tags:

```bash
make print-build-tags CONFIG_FILE=config/examples/config.sqlite.yaml
```

Build using a profile:

```bash
make build CONFIG_FILE=config/examples/config.postgres.yaml
```

Run tests using a profile:

```bash
make test-config CONFIG_FILE=config/examples/config.postgres.yaml
```

The default runtime configuration remains `config/config.yaml`.
