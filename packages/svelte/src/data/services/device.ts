// Moved from frontend/src/lib/services/device.ts, 2026-09-11
//
// The requests a screen makes about one printer. A service names the SDK
// operation and its parameters and holds no state (svelte-data.md).
import type { PrusaConnect, EventType } from '@prusa/connect';

import type { Device } from '../../domain/command';

/**
 * The type that says a read of one printer folder began or landed. A host
 * adds it to the one stream it opens.
 */
export const DEVICE_FILES_EVENTS: readonly EventType[] = [
  'com.prusa.connect.device.files.changed',
];

/** The print on the bed and the one before it, as the jobs operation answers. */
export type DeviceJobs = Awaited<ReturnType<PrusaConnect['getDeviceJobs']>>;

/** One page of the prints that have ended on a printer, newest first. */
export type DeviceJobPage = Awaited<ReturnType<PrusaConnect['listDeviceJobs']>>;

/** One directory of a printer's file tree, with the directory it names. */
export type DeviceListing = Awaited<
  ReturnType<PrusaConnect['listDeviceFiles']>
>;

/** One entry in that directory. */
export type DeviceEntry = NonNullable<DeviceListing['items']>[number];

/** What the core answers when it has accepted a command for the printer. */
export type CommandResult = Awaited<
  ReturnType<PrusaConnect['sendDeviceCommand']>
>;

/** The argument values a command carries, as the capability descriptor names them. */
export type CommandArgs = Readonly<Record<string, string>>;

/** What one printer's page reads beyond the fleet row. */
export type DeviceDetailClient = Pick<
  PrusaConnect,
  | 'getDevice'
  | 'getDeviceJobs'
  | 'listDeviceFiles'
  | 'listDeviceJobs'
  | 'listDeviceStorages'
  | 'url'
>;

/**
 * The address of the picture a printer reads out of one of its print files,
 * for an `img` tag, which sends the session cookie and no Authorization
 * header. The size and modification time the listing reported name the
 * version of the file, so the core answers a version it already holds without
 * asking the printer again.
 */
export const deviceThumbnailUrlOf = (
  client: Pick<PrusaConnect, 'url'>,
  deviceId: string,
  path: string,
  listed?: Pick<DeviceEntry, 'size' | 'modified'>,
): string => {
  const query = new URLSearchParams({ path });
  if (listed !== undefined) {
    query.set('size', String(listed.size));
    const seconds = Date.parse(listed.modified ?? '') / 1000;
    if (Number.isFinite(seconds)) {
      query.set('modified', String(Math.trunc(seconds)));
    }
  }

  return `${client.url}/api/v1/devices/${encodeURIComponent(deviceId)}/files/thumbnail?${query.toString()}`;
};

/** The newest prints that have ended on one printer, at most `limit` of them. */
export const readRecentJobs = (
  client: Pick<PrusaConnect, 'listDeviceJobs'>,
  deviceId: string,
  limit: number,
): Promise<DeviceJobPage> =>
  client.listDeviceJobs({ path: { device_id: deviceId }, query: { limit } });

/**
 * The printer's own record. The fleet listing leaves a retired printer out,
 * and this read still answers for it.
 */
export const readDevice = (
  client: DeviceDetailClient,
  deviceId: string,
): Promise<Device> => client.getDevice({ path: { device_id: deviceId } });

/** What sending a command needs of the SDK, and nothing more. */
export type CommandClient = Pick<PrusaConnect, 'sendDeviceCommand'>;

/**
 * The print on the bed and the most recent finished one. It is
 * `GET /api/v1/devices/{device_id}/jobs`, which answers those two rows and no
 * more: a page of finished prints waits on the core's `list-device-jobs`.
 */
export const readJobs = (
  client: DeviceDetailClient,
  deviceId: string,
): Promise<DeviceJobs> =>
  client.getDeviceJobs({ path: { device_id: deviceId } });

/**
 * One directory of the printer's own storage. An empty path lists the
 * storages themselves, which is where a person starts.
 *
 * The signal is the store's, so a listing a person has already clicked past is
 * stopped rather than left to answer into a screen showing another folder
 * (svelte-data.md).
 *
 * The server answers a path it listed before from memory and marks the answer
 * `stale`. `refresh` makes it ask the printer.
 */
export const readFiles = (
  client: DeviceDetailClient,
  deviceId: string,
  path: string,
  signal?: AbortSignal,
  refresh = false,
): Promise<DeviceListing> =>
  client.listDeviceFiles(
    {
      path: { device_id: deviceId },
      query: {
        ...(path === '' ? {} : { path }),
        ...(refresh ? { refresh } : {}),
      },
    },
    signal,
  );

/**
 * A new idempotency key. `crypto.randomUUID` exists only in a secure context,
 * and a server on a LAN address over plain HTTP is not one, so the key is
 * built from `getRandomValues`, which every context has.
 */
const freshKey = (): string =>
  Array.from(crypto.getRandomValues(new Uint8Array(16)), (byte) =>
    byte.toString(16).padStart(2, '0'),
  ).join('');

/**
 * Sends one command. The verb is a capability key the printer's own set
 * lists, and the core refuses any other, so nothing here decides whether the
 * machine will take it; the screen only decides what to offer.
 *
 * Each call carries its own idempotency key. A proxy that replays the request
 * while the first one still runs gets the first one's `command_id` back with
 * `duplicate` set, and the printer is sent the command once.
 */
export const sendCommand = (
  client: CommandClient,
  deviceId: string,
  verb: string,
  args?: CommandArgs,
): Promise<CommandResult> =>
  client.sendDeviceCommand({
    path: { device_id: deviceId },
    body:
      args === undefined
        ? { verb, key: freshKey() }
        : { verb, args: { ...args }, key: freshKey() },
  });
