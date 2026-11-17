import { Maintenance } from '@prusa/connect-ui/data';
import { describe, expect, it } from 'vitest';

import { json, stubClient } from '../test/stub';
import { readRestartAccess, restartAllowed } from './restart-access';

const maintenanceOver = (version: () => Response): Maintenance =>
  new Maintenance(
    stubClient({
      'GET /api/v1/server/version': version,
      'GET /api/v1/server/upgrade': () => json(404, {}),
      'GET /api/v1/server/upgrade/history': () => json(200, []),
    }).client,
  );

describe('restartAllowed', () => {
  it('is unknown until the maintenance read answers', () => {
    expect(
      restartAllowed(maintenanceOver(() => json(200, {}))),
    ).toBeUndefined();
  });

  it('is true once the read answered', async () => {
    const maintenance = maintenanceOver(() => json(200, { version: '1.0.0' }));
    await maintenance.load();

    expect(restartAllowed(maintenance)).toBe(true);
  });

  it('is false when the read is refused', async () => {
    const maintenance = maintenanceOver(() => json(403, {}));
    await maintenance.load();

    expect(restartAllowed(maintenance)).toBe(false);
  });
});

describe('readRestartAccess', () => {
  const over = (answer: () => Response) =>
    stubClient({ 'GET /api/v1/server/version': answer }).client;

  it.each([
    [200, true],
    [403, false],
    [500, undefined],
  ])('answers %s with %s', async (status, expected) => {
    expect(await readRestartAccess(over(() => json(status, {})))).toBe(
      expected,
    );
  });
});
