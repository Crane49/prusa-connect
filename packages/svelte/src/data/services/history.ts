// One printer's stored past. A service names the SDK operation and its
// parameters and keeps no state (svelte-data.md).
//
// The core keeps nothing by default: a deployment that declared no history
// store answers both of these with a refusal, and that refusal is a state the
// screen draws rather than an error to swallow.
import type { PrusaConnect, components } from '@prusa/connect';

/** What the history stores need of the SDK, and no more. */
export type HistoryClient = Pick<
  PrusaConnect,
  'listDeviceHistoryFacts' | 'listDeviceHistorySamples'
>;

export type HistoryFact = components['schemas']['HistoryFact'];
export type HistorySample = components['schemas']['HistorySample'];
export type HistoryFactPage = components['schemas']['HistoryFactPage'];
export type HistorySamplePage = components['schemas']['HistorySamplePage'];

/** The classes an event is filed under. */
export type HistoryClass = NonNullable<
  NonNullable<
    Parameters<PrusaConnect['listDeviceHistoryFacts']>[0]['query']
  >['classes']
>[number];

/** The span one read asks for, and where in it the read resumes. */
export interface HistoryWindow {
  /** RFC 3339, inclusive. */
  readonly from: string;
  /** RFC 3339, exclusive. */
  readonly to: string;
  readonly limit: number;
  /** Opaque text a previous page answered. Empty is the first page. */
  readonly cursor?: string;
  /** State paths to read, each matched whole. Omitted reads every metric. */
  readonly metrics?: readonly string[];
  /** Event types to read, each matched whole. Omitted reads every type. */
  readonly types?: readonly string[];
  /** Event classes to read. Omitted reads every class the caller may. */
  readonly classes?: readonly HistoryClass[];
}

const MS_PER_HOUR = 3_600_000;

/** An instant as the two routes spell one. */
export const instantOf = (at: number): string => new Date(at).toISOString();

/** The span ending now, as the two routes spell an instant. */
export const windowEndingNow = (
  hours: number,
): { from: string; to: string } => {
  const to = Date.now();

  return { from: instantOf(to - hours * MS_PER_HOUR), to: instantOf(to) };
};

// An empty cursor is left out rather than sent empty, which is what
// `readJobHistory` does for the same reason: the route reads a present cursor
// as a resume point. An empty narrowing is left out for the opposite reason:
// the routes read a present one as "only these", so sending nothing and
// sending an empty list mean different things.
const spanOf = (window: HistoryWindow) => {
  const { from, to, limit, cursor } = window;

  return cursor === undefined || cursor === ''
    ? { from, to, limit }
    : { from, to, limit, cursor };
};

const asked = <T>(values: readonly T[] | undefined): values is readonly T[] =>
  values !== undefined && values.length > 0;

export const readDeviceFacts = (
  client: HistoryClient,
  deviceId: string,
  window: HistoryWindow,
  signal?: AbortSignal,
): Promise<HistoryFactPage> =>
  client.listDeviceHistoryFacts(
    {
      path: { device_id: deviceId },
      query: {
        ...spanOf(window),
        ...(asked(window.types) ? { types: [...window.types] } : {}),
        ...(asked(window.classes) ? { classes: [...window.classes] } : {}),
      },
    },
    signal,
  );

export const readDeviceSamples = (
  client: HistoryClient,
  deviceId: string,
  window: HistoryWindow,
  signal?: AbortSignal,
): Promise<HistorySamplePage> =>
  client.listDeviceHistorySamples(
    {
      path: { device_id: deviceId },
      query: {
        ...spanOf(window),
        ...(asked(window.metrics) ? { metrics: [...window.metrics] } : {}),
      },
    },
    signal,
  );
