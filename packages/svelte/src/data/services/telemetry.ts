// One printer's live readings, read from the event stream. A service names the
// SDK operation and its parameters and keeps no state (svelte-data.md).
//
// The fleet store does not subscribe to telemetry, because four hundred
// printers report several times a second and the fleet list does not draw a
// reading. One printer's page subscribes for that printer alone.
import type { PrusaConnect, EventType, StreamEvent } from '@prusa/connect';

/** What the telemetry store needs of the SDK, and no more. */
export type TelemetryClient = Pick<PrusaConnect, 'events'>;

/** The one event that moves a reading. */
export const TELEMETRY_EVENTS: readonly EventType[] = [
  'com.prusa.connect.device.telemetry.sampled',
];

/**
 * A value a reading can take. The core sends whatever JSON the driver put at
 * the path, and the store keeps the scalars: a number for a chart, a string
 * for a state word, a boolean for a flag. An object or an array is skipped,
 * because neither is a row a table draws or a point a chart plots.
 */
export type TelemetryValue = number | string | boolean;

/** One reading: which printer, which path, and what it now reads. */
export interface TelemetrySample {
  readonly deviceId: string;
  /** The path in the printer's state tree, for example `/tools/0/nozzle_temp`. */
  readonly path: string;
  readonly value: TelemetryValue;
}

const SUBJECT_PREFIX = 'device:';

/** Which device an event is about. The envelope's subject is `kind:id`. */
const deviceIdOf = (event: StreamEvent): string | undefined => {
  const subject = event.envelope?.subject;
  if (subject?.startsWith(SUBJECT_PREFIX) === true) {
    return subject.slice(SUBJECT_PREFIX.length);
  }

  const payload = event.payload;
  if (typeof payload !== 'object' || payload === null) return undefined;
  // The payload crosses into this code as `unknown`, and this narrows it to
  // the one member being read (typescript.md).
  const named = (payload as { device_id?: unknown }).device_id;

  return typeof named === 'string' && named !== '' ? named : undefined;
};

const valueOf = (given: unknown): TelemetryValue | undefined => {
  if (typeof given === 'number')
    return Number.isFinite(given) ? given : undefined;
  if (typeof given === 'string' || typeof given === 'boolean') return given;

  return undefined;
};

/**
 * Reads one telemetry frame. It answers undefined for a frame that names no
 * printer, no path or no scalar value. The core publishes no payload schema,
 * so every member is read behind a check rather than trusted.
 */
export const sampleOf = (event: StreamEvent): TelemetrySample | undefined => {
  const deviceId = deviceIdOf(event);
  if (deviceId === undefined) return undefined;

  const payload = event.payload;
  if (typeof payload !== 'object' || payload === null) return undefined;

  const named = payload as { path?: unknown; value?: unknown };
  const path = typeof named.path === 'string' ? named.path : '';
  if (path === '') return undefined;

  const value = valueOf(named.value);
  if (value === undefined) return undefined;

  return { deviceId, path, value };
};
