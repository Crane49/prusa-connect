// How this server is doing: whether every required instance is serving, and
// how the printers behind each connector stand. A service names the SDK
// operations and keeps no state (svelte-data.md).
import type { PrusaConnect, components } from '@prusa/connect';

/** What the health store needs of the SDK, and no more. */
export type HealthClient = Pick<
  PrusaConnect,
  'getReadiness' | 'getConnectorHealth'
>;

export type ReadinessReport = components['schemas']['ReadinessReport'];
export type ConnectorRollup = components['schemas']['ConnectorRollup'];
export type ConnectorDevice = components['schemas']['ConnectorDevice'];

const SERVICE_UNAVAILABLE = 503;

// The narrowing reads two members off a value the server sent, which is a
// trust boundary (typescript.md).
const isReport = (value: unknown): value is ReadinessReport =>
  typeof value === 'object' &&
  value !== null &&
  typeof (value as { ready?: unknown }).ready === 'boolean' &&
  typeof (value as { serving?: unknown }).serving === 'number';

const reportIn = (thrown: unknown): ReadinessReport | undefined => {
  if (typeof thrown !== 'object' || thrown === null) return undefined;
  const refused: { status?: unknown; body?: unknown } = thrown;
  if (refused.status !== SERVICE_UNAVAILABLE) return undefined;
  if (typeof refused.body !== 'string') return undefined;

  try {
    const decoded: unknown = JSON.parse(refused.body);

    return isReport(decoded) ? decoded : undefined;
  } catch {
    return undefined;
  }
};

/**
 * The readiness verdict. The route answers 503 with the same report while a
 * required instance is down, and the SDK throws on every status that is not
 * 2xx, so that report is read back out of the refusal.
 */
export const readReadiness = async (
  client: HealthClient,
  signal?: AbortSignal,
): Promise<ReadinessReport> => {
  try {
    return await client.getReadiness({}, signal);
  } catch (thrown) {
    const report = reportIn(thrown);
    if (report === undefined) throw thrown;

    return report;
  }
};

/** One rollup per connector the caller has a printer on. */
export const readConnectorHealth = async (
  client: HealthClient,
  signal?: AbortSignal,
): Promise<readonly ConnectorRollup[]> =>
  (await client.getConnectorHealth({}, signal)).items ?? [];
