---
title: The firmware support matrix
description: Which command each printer model accepts and at which firmware version. Recorded by the team that wrote the firmware, and kept for the data.
audience: build
rewrite:
  describes: superseded
  implementedBy:
    - internal/printer/profile
    - internal/printer/capability
---

:::caution[Read this for the data, not for a design]
This page is what is left of a proposal for an `internal/commands/` package.
That package was never written, and capability descriptors merged by
intersection replace the mechanism it proposed.

One part is accurate and is why the page is kept. The model list, the
`device_type_id` map and the command-by-model matrix below are the firmware
support matrix as the team that wrote the firmware recorded it.

The current per-model verb set is generated from `internal/printer/profile` and
published at [capabilities](./capabilities.md). Where the two disagree, that
page is the one the running server answers from. The commands themselves go out
over the websocket from `internal/connector/xbuddy`.
:::

## Supported models and firmware

### Models that support websockets, firmware 6.4.0 and above

Derived from the firmware support matrix:

1. **Buddy firmware printers** (support websockets from 6.4.0):
   - **MK4** (device_type_id: 1.4.0)
   - **MK4S** (device_type_id: 1.4.1) - uses MK4 firmware
   - **MK3.9** (device_type_id: 1.3.9) - uses MK4 firmware
   - **MK3.9S** (device_type_id: 1.3.10) - uses MK4 firmware
   - **MK3.5** (device_type_id: 1.3.5)
   - **MK3.5S** (device_type_id: 1.3.6) - uses MK3.5 firmware
   - **MINI** (device_type_id: 2.1.0)
   - **XL** (device_type_id: 3.1.0)
   - **IX** (device_type_id: 4.1.0)
   - **CoreOne** (device_type_id: 7.1.0)
   - **CoreOneL** (device_type_id: 8.1.0)

2. **PrusaLink printers** (MK3, MK3S, MK2.5, MK2.5S):
   - These printers use PrusaLink firmware, not Buddy firmware
   - Websockets are not supported (use HTTP API)
   - **NOT SUPPORTED** in new Go server (only Buddy firmware)

### Minimum firmware version is 6.4.0

Websockets are integrated in firmware from version 6.4.0.

### Features by firmware version

From analysis of `printer_type.py`:

- **DIALOGS**: from 6.0.0.beta
- **TOOL_MAPPING**: from 6.1.0-RC (for MK4, MINI has from start)
- **SET_TOOL_PARAMS**: from 6.2.0.alpha
- **SET_ENCLOSURE_PARAMS**: from 6.2.0.alpha
- **SET_HOSTNAME**: from 6.2.0.alpha
- **OLD_JOB_INFO**: from 6.2.0.alpha
- **SET_ADDON_PARAMS**: from 6.3.0.alpha (only CoreOne)
- **SET_CHAMBER_PARAMS**: from start (HT90), otherwise from 6.2.0.alpha


### Commands by category

#### Basic print commands, every Buddy printer
- `START_PRINT` - start print
- `PAUSE_PRINT` - pause print
- `RESUME_PRINT` - resume print
- `STOP_PRINT` - stop print
- `SET_PRINTER_READY` - prepare printer
- `CANCEL_PRINTER_READY` - cancel ready state

#### Info commands, every Buddy printer
- `SEND_INFO` - get basic info
- `SEND_JOB_INFO` - get job info
- `SEND_FILE_INFO` - get file info
- `SEND_TRANSFER_INFO` - get transfer info
- `SEND_STATE_INFO` - get state info (requires DIALOGS feature)

#### File system commands, every Buddy printer with a filesystem
- `DELETE_FILE` - delete file
- `DELETE_FOLDER` - delete folder
- `CREATE_FOLDER` - create folder
- `START_CONNECT_DOWNLOAD` - download file from Connect
- `START_ENCRYPTED_DOWNLOAD` - download encrypted file
- `STOP_TRANSFER` - stop transfer

#### Gcode commands, every FDM printer
- `HOME` - homing
- `MESH_BED_LEVELING` - mesh bed leveling (G29 for Buddy)
- `RESET` - reset (M999 R)
- `DISABLE_STEPPERS` - disable motors (M84)
- `BEEP` - beep (M300)
- `MOVE_E` - extruder movement
- `MOVE` - X/Y movement
- `MOVE_Z` - Z movement

#### SET commands, every FDM printer
- `SET_SPEED` - set speed (M220)
- `SET_FLOW` - set flow (M221)
- `SET_NOZZLE_TEMPERATURE` - set nozzle temperature (M104)
- `SET_HEATBED_TEMPERATURE` - set heatbed temperature (M140)

#### SET_VALUE commands, by feature
`SET_VALUE` is the general command; the values it carries are below.

| value | firmware | feature required |
|---|---|---|
| `SET_TOOL_NOZZLE_DIAMETER` | 6.2.0.alpha | SET_TOOL_PARAMS |
| `SET_TOOL_HARDENED` | 6.2.0.alpha | SET_TOOL_PARAMS |
| `SET_TOOL_HIGH_FLOW` | 6.2.0.alpha | SET_TOOL_PARAMS |
| `SET_ENCLOSURE_ENABLED` | 6.2.0.alpha | SET_ENCLOSURE_PARAMS |
| `SET_ENCLOSURE_PRINTING_FILTRATION` | 6.2.0.alpha | SET_ENCLOSURE_PARAMS |
| `SET_ENCLOSURE_POSTPRINT` | 6.2.0.alpha | SET_ENCLOSURE_PARAMS |
| `SET_ENCLOSURE_POSTPRINT_FILTRATION_TIME` | 6.2.0.alpha | SET_ENCLOSURE_PARAMS |
| `SET_HOSTNAME` | 6.2.0.alpha | SET_HOSTNAME |
| `SET_CHAMBER_TARGET_TEMP` | 6.2.0.alpha, and HT90 from the start | SET_CHAMBER_PARAMS |
| `SET_CHAMBER_FAN_TARGET` | 6.3.0.alpha, CoreOne | SET_CHAMBER_PARAMS |
| `SET_ADDON_POWER` | 6.3.0.alpha, CoreOne | SET_ADDON_PARAMS |

#### Feature-specific commands
- `DIALOG_ACTION` (from 6.0.0.beta, requires DIALOGS)
- `CANCEL_OBJECT` (from 6.1.0, only Buddy printers)
- `UNCANCEL_OBJECT` (from 6.1.0, only Buddy printers)

#### Model-specific commands
- `Z_POWER_MOVE` (only IX)

#### Filament commands
- `LOAD_FILAMENT` - Buddy: `M701 S"PLA" W2`, PrusaLink: high-level
- `UNLOAD_FILAMENT` - Buddy: `M702 W2`, PrusaLink: high-level

`W2` makes the firmware preheat the nozzle first. Without it the printer waits
on the loading screen with a cold nozzle.

#### Upgrade commands
- `UPGRADE` - Buddy: M997 {path} (from 5.0.1 with path)
- `FLASH` - Buddy: M997 {path} (from 5.0.1 with path)

#### Reset commands
- `RESET_PRINTER` - software restart (from 0.7.0beta for PrusaLink, from 5.2.0-alpha1 for Buddy)


### Command by model, firmware 6.4.0 and above

| Command | MK4/MK4S/MK3.9/MK3.9S | MK3.5/MK3.5S | MINI | XL | IX | CoreOne/CoreOneL | Notes |
|---------|------------------------|---------------|------|----|----|------------------|----------|
| START_PRINT | yes | yes | yes | yes | yes | yes | TOOL_MAPPING from 6.1.0-RC (MK4) |
| PAUSE_PRINT | yes | yes | yes | yes | yes | yes | |
| RESUME_PRINT | yes | yes | yes | yes | yes | yes | |
| STOP_PRINT | yes | yes | yes | yes | yes | yes | |
| SET_PRINTER_READY | yes | yes | yes | yes | yes | yes | |
| CANCEL_PRINTER_READY | yes | yes | yes | yes | yes | yes | |
| SEND_INFO | yes | yes | yes | yes | yes | yes | |
| SEND_JOB_INFO | yes | yes | yes | yes | yes | yes | |
| SEND_FILE_INFO | yes | yes | yes | yes | yes | yes | |
| SEND_TRANSFER_INFO | yes | yes | yes | yes | yes | yes | |
| SEND_STATE_INFO | from 6.0.0.beta | from 6.0.0.beta | from 6.0.0.beta | from 6.0.0.beta | from 6.0.0.beta | from 6.0.0.beta | Requires DIALOGS |
| DELETE_FILE | yes | yes | yes | yes | yes | yes | |
| DELETE_FOLDER | yes | yes | yes | yes | yes | yes | |
| CREATE_FOLDER | yes | yes | yes | yes | yes | yes | |
| START_CONNECT_DOWNLOAD | yes | yes | yes | yes | yes | yes | |
| START_ENCRYPTED_DOWNLOAD | yes | yes | yes | yes | yes | yes | |
| STOP_TRANSFER | yes | yes | yes | yes | yes | yes | |
| HOME | yes | yes | yes | yes | yes | yes | |
| MESH_BED_LEVELING | yes | yes | yes | yes | yes | yes | |
| RESET | yes | yes | yes | yes | yes | yes | |
| DISABLE_STEPPERS | yes | yes | yes | yes | yes | yes | |
| BEEP | yes | yes | yes | yes | yes | yes | |
| MOVE_E | yes | yes | yes | yes | yes | yes | |
| MOVE | yes | yes | yes | yes | yes | yes | |
| MOVE_Z | yes | yes | yes | yes | yes | yes | |
| SET_SPEED | yes | yes | yes | yes | yes | yes | |
| SET_FLOW | yes | yes | yes | yes | yes | yes | |
| SET_NOZZLE_TEMPERATURE | yes | yes | yes | yes | yes | yes | |
| SET_HEATBED_TEMPERATURE | yes | yes | yes | yes | yes | yes | |
| LOAD_FILAMENT | yes | yes | yes | yes | yes | yes | |
| UNLOAD_FILAMENT | yes | yes | yes | yes | yes | yes | |
| UPGRADE | yes | yes | yes | yes | yes | yes | Path from 5.0.1 |
| FLASH | yes | yes | yes | yes | yes | yes | Path from 5.0.1 |
| RESET_PRINTER | yes | yes | yes | yes | yes | yes | |
| DIALOG_ACTION | from 6.0.0.beta | from 6.0.0.beta | from 6.0.0.beta | from 6.0.0.beta | from 6.0.0.beta | from 6.0.0.beta | Requires DIALOGS |
| CANCEL_OBJECT | from 6.1.0 | from 6.1.0 | no | from 6.1.0 | no | from 6.1.0 | |
| UNCANCEL_OBJECT | from 6.1.0 | from 6.1.0 | no | from 6.1.0 | no | from 6.1.0 | |
| SET_TOOL_NOZZLE_DIAMETER | from 6.2.0.alpha | from 6.2.0.alpha | from 6.2.0.alpha | from 6.2.0.alpha | from 6.2.0.alpha | from 6.2.0.alpha | Requires SET_TOOL_PARAMS |
| SET_TOOL_HARDENED | from 6.2.0.alpha | from 6.2.0.alpha | from 6.2.0.alpha | from 6.2.0.alpha | from 6.2.0.alpha | from 6.2.0.alpha | Requires SET_TOOL_PARAMS |
| SET_TOOL_HIGH_FLOW | from 6.2.0.alpha | from 6.2.0.alpha | from 6.2.0.alpha | from 6.2.0.alpha | from 6.2.0.alpha | from 6.2.0.alpha | Requires SET_TOOL_PARAMS |
| SET_ENCLOSURE_ENABLED | from 6.2.0.alpha | from 6.2.0.alpha | from 6.2.0.alpha | from 6.2.0.alpha | from 6.2.0.alpha | from 6.2.0.alpha | Requires SET_ENCLOSURE_PARAMS |
| SET_ENCLOSURE_PRINTING_FILTRATION | from 6.2.0.alpha | from 6.2.0.alpha | from 6.2.0.alpha | from 6.2.0.alpha | from 6.2.0.alpha | from 6.2.0.alpha | Requires SET_ENCLOSURE_PARAMS |
| SET_ENCLOSURE_POSTPRINT | from 6.2.0.alpha | from 6.2.0.alpha | from 6.2.0.alpha | from 6.2.0.alpha | from 6.2.0.alpha | from 6.2.0.alpha | Requires SET_ENCLOSURE_PARAMS |
| SET_ENCLOSURE_POSTPRINT_FILTRATION_TIME | from 6.2.0.alpha | from 6.2.0.alpha | from 6.2.0.alpha | from 6.2.0.alpha | from 6.2.0.alpha | from 6.2.0.alpha | Requires SET_ENCLOSURE_PARAMS |
| SET_HOSTNAME | from 6.2.0.alpha | from 6.2.0.alpha | from 6.2.0.alpha | from 6.2.0.alpha | from 6.2.0.alpha | from 6.2.0.alpha | Requires SET_HOSTNAME |
| SET_CHAMBER_TARGET_TEMP | no | no | no | no | no | from 6.2.0.alpha | Only CoreOne, requires SET_CHAMBER_PARAMS |
| SET_CHAMBER_FAN_TARGET | no | no | no | no | no | from 6.3.0.alpha | Only CoreOne, requires SET_CHAMBER_PARAMS |
| SET_ADDON_POWER | no | no | no | no | no | from 6.3.0.alpha | Only CoreOne, requires SET_ADDON_PARAMS |
| Z_POWER_MOVE | no | no | no | no | yes | no | Only IX |

### Legend

`yes` and `no` are for firmware 6.4.0 and above. A cell reading `from 6.2.0.alpha`
means the command arrives at that firmware version and not before.

### Notes on reading the table

1. **Minimum firmware version.** All printers must have firmware **6.4.0 or higher** for websocket support.

2. **Shared firmware.** Some models run another model's firmware:
   - MK4, MK4S, MK3.9 and MK3.9S run MK4 firmware, 1.4.0
   - MK3.5 and MK3.5S run MK3.5 firmware, 1.3.5

3. **Feature detection.** Features are detected when printer connects from INFO event. If feature is not available, corresponding commands will not be in registry.

4. **Firmware version format.** Firmware versions are in format `X.Y.Z` or `X.Y.Z-suffix` (e.g., `6.4.0`, `6.2.0.alpha`, `6.1.0-RC`).
