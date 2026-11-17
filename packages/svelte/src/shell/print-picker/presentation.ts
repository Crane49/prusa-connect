// Which printers can take a file, and what to say about the ones that cannot.
import type { RunningTransferRow } from '../../data/running-transfers.svelte';
import { START_PRINT_VERB } from '../../data/services/print';
import {
  printerStateOf,
  projectCommand,
  STATE_PATH,
  textAt,
  type Device,
  type PrinterState,
  type StateTransition,
} from '../../domain';
import type { PrintTarget } from './types';

/**
 * The states a printer starts a print from when its device kind declares no
 * moves between states. The bed is free in both: the machine is waiting, or it
 * has been made ready.
 *
 * FINISHED and STOPPED are not here. Both say a print ended, and the part is
 * still on the bed until somebody takes it off. The firmware accepts a start
 * in either, so a picker that offered them would print into the last part.
 * `internal/app/devicejobs.go` names IDLE and READY as the free bed for the
 * same reason.
 *
 * A word this library has no rule for reads as UNKNOWN and is not offered.
 * That is the safe way round.
 */
export const FREE_STATES: readonly PrinterState[] = ['IDLE', 'READY'];

const FREE: ReadonlySet<string> = new Set(FREE_STATES);

/** Why a printer cannot take a file, as a catalogue key. */
export const REASON_KEY = {
  offline: 'printPicker.reason.state.OFFLINE',
  noVerb: 'printPicker.reason.noVerb',
  locked: 'printPicker.reason.locked',
  noStorage: 'printPicker.reason.noStorage',
} as const;

/**
 * The sentence each state gets, so a reader is told what the machine is doing
 * and not only that it is unavailable. A state the picker offers has no line,
 * because there is nothing to explain about a printer that can take the file.
 */
export const STATE_REASON_KEY: Record<PrinterState, string | undefined> = {
  ATTENTION: 'printPicker.reason.state.ATTENTION',
  BUSY: 'printPicker.reason.state.BUSY',
  ERROR: 'printPicker.reason.state.ERROR',
  FINISHED: 'printPicker.reason.state.FINISHED',
  IDLE: undefined,
  MANIPULATING: 'printPicker.reason.state.MANIPULATING',
  OFFLINE: 'printPicker.reason.state.OFFLINE',
  PAUSED: 'printPicker.reason.state.PAUSED',
  PRINTING: 'printPicker.reason.state.PRINTING',
  READY: undefined,
  STOPPED: 'printPicker.reason.state.STOPPED',
  UNKNOWN: 'printPicker.reason.state.UNKNOWN',
};

/** What the picker is told about the printers beyond the records themselves. */
export interface PrintTargetOptions {
  /**
   * The moves between states the device kind declared, as
   * `GET /api/v1/vocabularies/device-states` publishes them. A kind that names
   * the start verb decides on its own words; one that names none falls to
   * `FREE_STATES`.
   */
  readonly transitions?: readonly StateTransition[] | undefined;
  /** The printers held by another operator or by a command still running. */
  readonly locked?: readonly string[] | undefined;
  /**
   * The printers that answered and have no storage they can write to, which
   * is no USB drive in them. A file from this server has nowhere to land.
   */
  readonly noStorage?: readonly string[] | undefined;
}

/**
 * A disconnected machine reports nothing, so its state is read as OFFLINE
 * rather than as the last word it said before the session closed. That is the
 * printers listing's rule, written again here over the fields the picker draws.
 */
const wireStateOf = (device: Device): string | undefined => {
  if (!device.connected) return 'OFFLINE';

  return textAt(device, STATE_PATH);
};

const declaresStart = (transitions: readonly StateTransition[]): boolean =>
  transitions.some((move) => move.verb === START_PRINT_VERB);

/** The line a blocked printer shows: its key, and the states the key names. */
interface Reason {
  readonly key: string;
  readonly states?: readonly string[] | undefined;
}

const reasonOf = (
  device: Device,
  state: PrinterState,
  wire: string | undefined,
  options: PrintTargetOptions,
): Reason | undefined => {
  if (!device.connected) return { key: REASON_KEY.offline };
  if (options.noStorage?.includes(device.id) === true) {
    return { key: REASON_KEY.noStorage };
  }

  const transitions = options.transitions ?? [];
  const projection = projectCommand(START_PRINT_VERB, {
    capabilities: device.capabilities,
    transitions,
    state: wire,
    locked: options.locked?.includes(device.id) ?? false,
    permitted: device.verbs,
  });

  if (projection.kind === 'absent') return { key: REASON_KEY.noVerb };
  if (projection.kind === 'disabled') {
    // The verb table already refused it: the printer is held, it has said
    // nothing yet, or its kind allows the start from other states. Each of
    // those carries its own line, and the states one names them.
    if (projection.reason.key === 'command.refused.locked') {
      return { key: REASON_KEY.locked };
    }

    return { key: projection.reason.key, states: projection.reason.states };
  }

  // The kind published moves for the start verb, so its own table decided and
  // there is nothing left to add.
  if (declaresStart(transitions)) return undefined;
  if (FREE.has(state)) return undefined;

  const key = STATE_REASON_KEY[state];

  return key === undefined ? undefined : { key };
};

export const printTargetOf = (
  device: Device,
  options: PrintTargetOptions = {},
): PrintTarget => {
  const wire = wireStateOf(device);
  const state = printerStateOf(wire ?? 'UNKNOWN');
  const reason = reasonOf(device, state, wire, options);

  return {
    id: device.id,
    model: device.model,
    name: device.name,
    ready: reason === undefined,
    reasonKey: reason?.key,
    reasonStates: reason?.states,
    state,
  };
};

/**
 * Every printer, the ones that can take the file first and each group by name.
 * A person opens this picker to start a print, so the machines that can start
 * one are what the list opens on; the rest stay, because a reader who came for
 * a named printer has to find out why it is not offered.
 */
export const printTargetsOf = (
  devices: readonly Device[],
  options: PrintTargetOptions = {},
): PrintTarget[] =>
  devices
    .map((device) => printTargetOf(device, options))
    .sort(
      (a, b) =>
        Number(b.ready) - Number(a.ready) || a.name.localeCompare(b.name),
    );

/** A model name with case and punctuation dropped, so `Core One` meets `COREONE`. */
const modelKey = (model: string): string =>
  model.toLowerCase().replace(/[^a-z0-9]/g, '');

/**
 * True when the file names the printer it was sliced for and this printer is
 * another model. It is a warning and never a block: a printer of a close
 * model may print the file well, and the person sending it knows which.
 */
export const slicedForOther = (
  target: Pick<PrintTarget, 'model'>,
  slicedFor: string | undefined,
): boolean =>
  slicedFor !== undefined &&
  slicedFor !== '' &&
  target.model !== '' &&
  modelKey(target.model) !== modelKey(slicedFor);

/**
 * Where a file on its way to one printer is. `sending` while the bytes move,
 * with how far they got from 0 to 1, or null while the length is unknown.
 * `writing` once every byte is out: the printer still writes the file to its
 * drive before it reports the transfer finished, which on an MK4S takes most
 * of a minute for a file of a few megabytes.
 */
export type PrintPhase =
  | { readonly step: 'sending'; readonly progress: number | null }
  | { readonly step: 'writing' };

/** The phase of one running transfer, or undefined when none is running. */
export const printPhaseOf = (
  row: Pick<RunningTransferRow, 'bytesSent' | 'bytesTotal'> | undefined,
): PrintPhase | undefined => {
  if (row === undefined) return undefined;
  if (row.bytesTotal <= 0) return { step: 'sending', progress: null };
  if (row.bytesSent >= row.bytesTotal) return { step: 'writing' };

  return { step: 'sending', progress: row.bytesSent / row.bytesTotal };
};
