// The one request that reads why a printer can carry an issue. A service names
// the SDK operation and its parameters and holds no state (svelte-data.md).
import type { PrusaConnect } from '@prusa/connect';

/** What the device-issue vocabulary needs of the SDK, and no more. */
export type DeviceIssuesClient = Pick<PrusaConnect, 'getDeviceIssueVocabulary'>;

/** The answer `GET /api/v1/vocabularies/device-issues` gives. */
export type DeviceIssuesAnswer = Awaited<
  ReturnType<PrusaConnect['getDeviceIssueVocabulary']>
>;

/** One reason: its severity, what is wrong, and what a person does about it. */
export type DeviceIssueEntry = NonNullable<DeviceIssuesAnswer['items']>[number];

export const readDeviceIssues = (
  client: DeviceIssuesClient,
): Promise<DeviceIssuesAnswer> => client.getDeviceIssueVocabulary();
