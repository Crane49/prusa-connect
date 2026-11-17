// The requests behind per-printer access: the grants written on one printer,
// the grants written for one person, and the writes that change them.
import type { PrusaConnect } from '@prusa/connect';

import type { AccessStep } from '../../domain/printer-access';

/** One grant as the server spells it: `device:<id>`, a relation, `user:<id>`. */
export type Grant = NonNullable<
  Awaited<ReturnType<PrusaConnect['listGrants']>>['items']
>[number];

/** What the grants store needs of the SDK. */
export type GrantsClient = Pick<
  PrusaConnect,
  'listGrants' | 'listSubjectDirectGrants' | 'writeGrant' | 'withdrawGrant'
>;

/** Every grant written on one printer. Only its administrator may read them. */
export const readDeviceGrants = async (
  client: GrantsClient,
  deviceId: string,
  signal?: AbortSignal,
): Promise<readonly Grant[]> =>
  (
    await client.listGrants(
      { path: { object_type: 'device', object_id: deviceId } },
      signal,
    )
  ).items ?? [];

/** Every grant written on a printer for one subject, on printers the caller administers. */
export const readSubjectGrants = async (
  client: GrantsClient,
  subject: string,
  signal?: AbortSignal,
): Promise<readonly Grant[]> =>
  (await client.listSubjectDirectGrants({ path: { subject } }, signal)).items ??
  [];

/**
 * Runs the steps in order and stops at the first refusal, which it throws.
 * The order is the plan's: writes before withdrawals.
 */
export const applyAccessSteps = async (
  client: GrantsClient,
  deviceId: string,
  subject: string,
  steps: readonly AccessStep[],
): Promise<void> => {
  const path = { object_type: 'device' as const, object_id: deviceId };
  for (const step of steps) {
    if (step.op === 'write') {
      await client.writeGrant({
        path,
        body: { relation: step.relation, subject },
      });
    } else {
      await client.withdrawGrant({
        path,
        query: { relation: step.relation, subject },
      });
    }
  }
};
