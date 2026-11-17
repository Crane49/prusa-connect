// The one request that reads the states each device kind declared. A service
// names the SDK operation and its parameters and holds no state
// (svelte-data.md).
import type { PrusaConnect } from '@prusa/connect';

/** What the device-state vocabulary needs of the SDK, and no more. */
export type DeviceStatesClient = Pick<PrusaConnect, 'getDeviceStateVocabulary'>;

/** The answer `GET /api/v1/vocabularies/device-states` gives. */
export type DeviceStatesAnswer = Awaited<
  ReturnType<PrusaConnect['getDeviceStateVocabulary']>
>;

/** One device kind: the states it reports and the moves between them. */
export type DeviceStateKind = NonNullable<DeviceStatesAnswer['items']>[number];

/**
 * What this deployment's adapters declared. It is what the server has
 * installed rather than what the build ships, so two servers on one release
 * answer differently and a client reads it per server.
 */
export const readDeviceStates = (
  client: DeviceStatesClient,
): Promise<DeviceStatesAnswer> => client.getDeviceStateVocabulary();
