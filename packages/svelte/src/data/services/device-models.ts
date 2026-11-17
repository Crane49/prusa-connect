// The one request that reads the models this build carries a profile for. A
// service names the SDK operation and its parameters and holds no state
// (svelte-data.md).
import type { PrusaConnect } from '@prusa/connect';

/** What the device-model vocabulary needs of the SDK, and no more. */
export type DeviceModelsClient = Pick<PrusaConnect, 'getDeviceModelVocabulary'>;

/** The answer `GET /api/v1/vocabularies/device-models` gives. */
export type DeviceModelsAnswer = Awaited<
  ReturnType<PrusaConnect['getDeviceModelVocabulary']>
>;

/** One model: its spelling, its name, and what its slots are. */
export type DeviceModelEntry = NonNullable<DeviceModelsAnswer['items']>[number];

export const readDeviceModels = (
  client: DeviceModelsClient,
): Promise<DeviceModelsAnswer> => client.getDeviceModelVocabulary();
