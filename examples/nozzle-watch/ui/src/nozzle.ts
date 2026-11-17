// Where the nozzle's readings are in a device record, and how they are read
// and worded out of it. `nozzle-section` and `nozzle-line` both draw a
// temperature and a target from the same state tree, in the same words, so
// both narrowing and wording live here rather than once per component.
//
// A device record carries `state`, the addressable state tree keyed by path,
// and the host writes a telemetry sample into it under the metric's own path.
// The values are `unknown` there, because the core publishes no schema for the
// tree: a driver declares the paths it reports. So each one is narrowed here
// rather than trusted (typescript.md), and a printer that has reported nothing
// reads as `undefined` rather than as zero degrees.
import type { Device, HeaterReading } from '@prusa/connect-ui/domain';
import { formatNumber, type Translate } from '@prusa/connect-ui/i18n';

// The printer's tools are a tree keyed by tool id, and the first head is `0`
// (ADR-20261023-gna). A printer with one nozzle is that head (ADR-20261025-pze). This module
// watches that head.

/** What the printer says the first head's nozzle is. */
export const NOZZLE = '/tools/0/nozzle_temp';

/** What the printer was told to take it to. Zero means the heater is off. */
export const NOZZLE_TARGET = '/tools/0/nozzle_target_temp';

/** The first head's nozzle diameter, when a driver reports it. Not every driver does. */
export const NOZZLE_DIAMETER = '/tools/0/features/nozzle_diameter';

/** The material loaded in the first head, when a driver reports it. Not every driver does. */
export const LOADED_MATERIAL =
  '/tools/0/material_package_instance/package/material/abbreviation';

/** A number at one path in a device's state tree, or `undefined` past a printer that has reported nothing there, or a value that is not one. */
export const numberAt = (
  device: Device | undefined,
  path: string,
): number | undefined => {
  const value = device?.state[path];

  return typeof value === 'number' && Number.isFinite(value)
    ? value
    : undefined;
};

/** A string at one path in a device's state tree, or `undefined` past nothing there, or an empty one. */
export const stringAt = (
  device: Device | undefined,
  path: string,
): string | undefined => {
  const value = device?.state[path];

  return typeof value === 'string' && value !== '' ? value : undefined;
};

/**
 * The nozzle's reading, in the shape the domain's heating rules take. A
 * printer that is not in the fleet, or one that has reported no temperature,
 * gives a reading of two `undefined`s, which `heaterState` reads as
 * `not-reported`.
 */
export const nozzleOf = (device: Device | undefined): HeaterReading => ({
  current: numberAt(device, NOZZLE),
  target: numberAt(device, NOZZLE_TARGET),
});

/**
 * A Celsius reading the way every screen of this module writes one: the
 * degree sign and the decimal mark are the locale's, so `Intl` writes the
 * reading (frontend-i18n.md rule 3).
 */
export const celsius = (value: number, decimals: number): string =>
  formatNumber(value, {
    style: 'unit',
    unit: 'celsius',
    unitDisplay: 'short',
    minimumFractionDigits: decimals,
    maximumFractionDigits: decimals,
  });

/**
 * The target's own text, the way every screen of this module writes it: "no
 * target" past zero or absent, "target <value>" otherwise. Shared so the
 * section and the card line never say the same fact two different ways.
 */
export const targetTextOf = (
  target: number | undefined,
  t: Translate,
): string =>
  target === undefined || target <= 0
    ? t('nozzle.noTarget')
    : t('nozzle.target', { value: celsius(target, 0) });
