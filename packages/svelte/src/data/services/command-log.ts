// One printer's stored commands. A service names the SDK operation and its
// parameters and keeps no state (svelte-data.md).
//
// A deployment that declared no history store keeps no command record and
// answers both reads with the same refusal the history routes give.
import type { PrusaConnect, components } from '@prusa/connect';

/** What the command log needs of the SDK, and no more. */
export type CommandLogClient = Pick<
  PrusaConnect,
  'listDeviceCommands' | 'getCommand'
>;

/**
 * One invocation as the server folded it. The three audit members reach only
 * a caller who holds `view_audit` on the device.
 */
export type CommandRecord = components['schemas']['HistoryCommand'];
export type CommandRecordPage = components['schemas']['HistoryCommandPage'];

/** The newest commands first, from where an earlier page stopped. */
export const readDeviceCommands = (
  client: CommandLogClient,
  deviceId: string,
  cursor: string,
  signal?: AbortSignal,
): Promise<CommandRecordPage> =>
  client.listDeviceCommands(
    {
      path: { device_id: deviceId },
      // The route reads a present cursor as a resume point, so an empty one
      // is left out.
      ...(cursor === '' ? {} : { query: { cursor } }),
    },
    signal,
  );

export const readCommand = (
  client: Pick<CommandLogClient, 'getCommand'>,
  commandId: string,
  signal?: AbortSignal,
): Promise<CommandRecord> =>
  client.getCommand({ path: { command_id: commandId } }, signal);
