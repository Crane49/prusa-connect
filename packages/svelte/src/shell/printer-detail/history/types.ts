import type {
  DeviceCommandLog,
  DeviceFacts,
  DeviceSamples,
  Transfers,
} from '../../../data';
import type { TelemetryPoint } from '../../../domain';

export type FactsSource = Pick<
  DeviceFacts,
  'rows' | 'loading' | 'loaded' | 'error' | 'incomplete' | 'more'
>;

export type SamplesSource = Pick<
  DeviceSamples,
  | 'series'
  | 'since'
  | 'loading'
  | 'loaded'
  | 'error'
  | 'truncated'
  | 'incomplete'
>;

/** The live readings this page already holds, to continue each line with. */
export interface LiveSource {
  /** Every path the printer has reported this session, as its keys. */
  readonly values: Readonly<Record<string, unknown>>;
  points(metric: string): readonly TelemetryPoint[];
}

export interface PrinterHistoryPanelProps {
  readonly facts: FactsSource;
  readonly samples: SamplesSource;
  readonly live?: LiveSource | undefined;
  /**
   * The printer's state tree as the server holds it, keyed by path. The
   * legend offers each reading in it, including the ones the read skipped.
   */
  readonly reported?: Readonly<Record<string, unknown>> | undefined;
  /** The span the two reads asked for, in milliseconds, for the time axis. */
  readonly span?: { readonly from: number; readonly to: number } | undefined;
  /** The metrics to draw. Left out, the panel chooses the usual ones. */
  readonly drawn?: readonly string[] | undefined;
  /** What an event type means, in the server's own words. */
  readonly describeType?: ((type: string) => string) | undefined;
  /** Called with the metrics a reader turned on, for a host that keeps them. */
  readonly onDrawnChange?: ((next: readonly string[]) => void) | undefined;
  readonly onMore?: (() => void) | undefined;
  /** Called when the tab opens, with the readings it will draw. */
  readonly onOpen?: ((metrics: readonly string[]) => void) | undefined;
  /** Called when the tab closes, so a read in flight can be dropped. */
  readonly onClose?: (() => void) | undefined;
}

/** What the command log reads of its store. */
export type CommandLogSource = Pick<
  DeviceCommandLog,
  | 'rows'
  | 'loading'
  | 'loaded'
  | 'error'
  | 'more'
  | 'record'
  | 'recordError'
  | 'load'
  | 'loadMore'
  | 'open'
  | 'abort'
>;

export interface CommandLogProps {
  readonly source: CommandLogSource;
}

/**
 * What the transfer log reads of the printer's transfers store. The page
 * already reads and follows that store, so the log only draws it.
 */
export type TransferLogSource = Pick<Transfers, 'rows' | 'loaded' | 'error'>;

export interface TransferLogProps {
  readonly source: TransferLogSource;
}
