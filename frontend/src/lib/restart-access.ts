import type { PrusaConnect } from '@prusa/connect';
import { faultOf, type Maintenance } from '@prusa/connect-ui/data';

/**
 * Whether this person may restart the server, as the maintenance read already
 * answers it: the restart sits behind the same administrator gate, so a 403
 * there is a 403 here. Undefined until that read has answered.
 */
export const restartAllowed = (
  maintenance: Maintenance,
): boolean | undefined => {
  if (maintenance.error?.status === 403) return false;

  return maintenance.loaded && maintenance.error === undefined
    ? true
    : undefined;
};

/**
 * Asks once whether this person may restart the server, with the cheapest read
 * behind the same administrator gate: the server version. A refusal says no;
 * any other failure leaves the answer unknown.
 */
export const readRestartAccess = async (
  client: Pick<PrusaConnect, 'getServerVersion'>,
): Promise<boolean | undefined> => {
  try {
    await client.getServerVersion();

    return true;
  } catch (thrown) {
    return faultOf(thrown).status === 403 ? false : undefined;
  }
};
