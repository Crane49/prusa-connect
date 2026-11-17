// Raw G-code, for the developer tools page. A service names the SDK operation
// and its parameters and keeps no state (svelte-data.md).
//
// G-code is the text a printer runs. `send-printer-gcode` is
// `POST /api/v1/devices/{device_id}/gcode`, and it answers one step per line
// with the printer's own answer on it.
import type { PrusaConnect } from '@prusa/connect';

/** What the G-code route answers: how far it got, and one step per line. */
export type GcodeOutcome = Awaited<
  ReturnType<PrusaConnect['sendPrinterGcode']>
>;

/** One line's outcome: `accepted`, `refused` or `not_attempted`, and the answer. */
export type GcodeStep = NonNullable<GcodeOutcome['steps']>[number];

/** The stored presets, as `list-gcode-presets` answers. */
export type GcodePresetPage = Awaited<
  ReturnType<PrusaConnect['listGcodePresets']>
>;

/** One stored preset a person runs by name. */
export type GcodePreset = NonNullable<GcodePresetPage['items']>[number];

/** What the G-code console needs of the SDK, and no more. */
export type GcodeClient = Pick<
  PrusaConnect,
  'sendPrinterGcode' | 'listGcodePresets' | 'runPrinterGcodePreset'
>;

/**
 * Splits what a person typed into lines. A blank line and a line that is only
 * a comment are dropped, so a pasted file does not spend a step on each.
 */
export const gcodeLines = (text: string): readonly string[] =>
  text
    .split('\n')
    .map((line) => line.trim())
    .filter((line) => line !== '' && !line.startsWith(';'));

/**
 * Sends the lines in order. The route stops at the first refusal unless
 * `force` is set, and `stopped` on the answer says whether it did.
 */
export const sendGcode = (
  client: GcodeClient,
  deviceId: string,
  lines: readonly string[],
  force = false,
): Promise<GcodeOutcome> =>
  client.sendPrinterGcode({
    path: { device_id: deviceId },
    body: { lines: [...lines], force },
  });

/** Every preset this caller may run. */
export const readGcodePresets = (
  client: GcodeClient,
): Promise<GcodePresetPage> => client.listGcodePresets();

/** Runs one stored preset against one printer. */
export const runGcodePreset = (
  client: GcodeClient,
  deviceId: string,
  name: string,
): Promise<GcodeOutcome> =>
  client.runPrinterGcodePreset({
    path: { device_id: deviceId, preset: name },
  });
