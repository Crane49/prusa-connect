// The requests about the local network: the printers this server hears, and
// whether it announces itself. A service names the SDK operation and its
// parameters and keeps no state (svelte-data.md).
import type { PrusaConnect } from '@prusa/connect';

/** What the discovery stores need of the SDK, and no more. */
export type DiscoveryClient = Pick<
  PrusaConnect,
  'listNearbyPrinters' | 'getDiscoveryVisibility' | 'setDiscoveryVisibility'
>;

/** The printers nearby, as `list-nearby-printers` answers them. */
export type NearbyPage = Awaited<
  ReturnType<PrusaConnect['listNearbyPrinters']>
>;

/** One printer this server hears: its name, its address and its own page. */
export type NearbyPrinter = NonNullable<NearbyPage['items']>[number];

/** Whether this server is hidden, and what its announcer is doing. */
export type Visibility = Awaited<
  ReturnType<PrusaConnect['getDiscoveryVisibility']>
>;

/** Every printer this server hears on the local network. */
export const readNearby = (client: DiscoveryClient): Promise<NearbyPage> =>
  client.listNearbyPrinters();

/** The hidden setting as the running server applies it. */
export const readVisibility = (client: DiscoveryClient): Promise<Visibility> =>
  client.getDiscoveryVisibility();

/**
 * Hides this server from the local network, or shows it again. The answer is
 * the setting as the server applied it.
 */
export const writeVisibility = (
  client: DiscoveryClient,
  hidden: boolean,
): Promise<Visibility> => client.setDiscoveryVisibility({ body: { hidden } });

/** What the time server store needs of the SDK, and no more. */
export type TimeServerClient = Pick<
  PrusaConnect,
  'getTimeServer' | 'setTimeServer'
>;

/** The time server switch and what the time server is doing. */
export type TimeServerState = Awaited<
  ReturnType<PrusaConnect['getTimeServer']>
>;

/** The time server switch as the running server applies it. */
export const readTimeServer = (
  client: TimeServerClient,
): Promise<TimeServerState> => client.getTimeServer();

/** Starts or stops the time server. The answer is the switch as applied. */
export const writeTimeServer = (
  client: TimeServerClient,
  enabled: boolean,
): Promise<TimeServerState> => client.setTimeServer({ body: { enabled } });
