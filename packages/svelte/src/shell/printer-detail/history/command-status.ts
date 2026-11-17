// How one stored command ended, as a word and a badge colour.
import type { CommandRecord } from '../../../data';
import { formatDuration, formatNumber, t } from '../../../i18n';
import type { BadgeVariant } from '../../../primitives';

/** The words the server documents for how a command ended. */
const STATUSES = [
  'succeeded',
  'failed',
  'refused',
  'timed_out',
  'abandoned',
  'unauthorized',
  'server_restarted',
] as const;

type Status = (typeof STATUSES)[number];

/** What a row's result column can say. */
export type CommandResult = Status | 'open' | 'unknown' | 'other';

const KNOWN: ReadonlySet<string> = new Set(STATUSES);

const isStatus = (word: string): word is Status => KNOWN.has(word);

/**
 * An unknown outcome outranks the status: the server says it never saw how the
 * command ended, so a status beside it would claim more than it knows. The
 * status is an open set, and a word a later build adds is `other`, drawn as
 * the server spelled it.
 */
export const resultOf = (
  record: Pick<CommandRecord, 'status' | 'outcome_unknown'>,
): CommandResult => {
  if (record.outcome_unknown === true) return 'unknown';
  if (record.status === undefined || record.status === '') return 'open';

  return isStatus(record.status) ? record.status : 'other';
};

/** Every key is written out rather than assembled from a value. */
export const RESULT_KEY: Readonly<
  Record<Exclude<CommandResult, 'other'>, string>
> = {
  open: 'printerDetailHistory.commands.result.open',
  unknown: 'printerDetailHistory.commands.result.unknown',
  succeeded: 'printerDetailHistory.commands.result.succeeded',
  failed: 'printerDetailHistory.commands.result.failed',
  refused: 'printerDetailHistory.commands.result.refused',
  timed_out: 'printerDetailHistory.commands.result.timedOut',
  abandoned: 'printerDetailHistory.commands.result.abandoned',
  unauthorized: 'printerDetailHistory.commands.result.unauthorized',
  server_restarted: 'printerDetailHistory.commands.result.serverRestarted',
};

export const RESULT_VARIANT: Readonly<Record<CommandResult, BadgeVariant>> = {
  open: 'secondary',
  unknown: 'warning',
  succeeded: 'success',
  failed: 'destructive',
  refused: 'destructive',
  timed_out: 'destructive',
  abandoned: 'warning',
  unauthorized: 'destructive',
  server_restarted: 'warning',
  other: 'secondary',
};

/** How a command ended, as a word a person reads. */
export const resultText = (
  record: Pick<CommandRecord, 'status' | 'outcome_unknown'>,
): string => {
  const result = resultOf(record);

  return result === 'other' ? (record.status ?? '') : t(RESULT_KEY[result]);
};

// Above a second the time reads in seconds: "18,845 ms" in a locale that
// groups thousands with a dot reads as eighteen milliseconds.
export const elapsedText = (ms: number): string =>
  ms < 1000
    ? formatNumber(ms, {
        style: 'unit',
        unit: 'millisecond',
        unitDisplay: 'short',
      })
    : formatDuration(ms / 1000);
