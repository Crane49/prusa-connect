import { expect, test, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';

import { appContext } from '../../test/app-context';
import { json } from '../../test/stub';
import { PATHS } from '../routes';
import Server from './server.svelte';

// The settings are read again while the Server room is open. A read that
// keeps the switch as it was loads nothing more, and one that turns
// upgrades on loads the upgrade reads the room has not made yet.
test('the Server room loads the upgrade reads again only when the switch changes', async () => {
  let upgrades = false;
  let history = 0;
  const ctx = appContext(
    {
      'GET /api/v1/ui': () => json(200, { upgrades }),
      'GET /api/v1/server/version': () => json(200, { version: '1.2.0' }),
      'GET /api/v1/server/upgrade/history': () => {
        history += 1;
        return json(200, []);
      },
    },
    { path: PATHS.settingsServerOverview },
  );
  await ctx.settings.read();
  await render(Server, { props: { ctx } });
  await vi.waitFor(() => {
    expect(ctx.maintenance.loaded).toBe(true);
  });

  await ctx.settings.read();
  await new Promise((resolve) => setTimeout(resolve, 100));
  expect(history).toBe(0);

  upgrades = true;
  await ctx.settings.read();
  await vi.waitFor(() => {
    expect(history).toBe(1);
  });

  await ctx.settings.read();
  await new Promise((resolve) => setTimeout(resolve, 100));
  expect(history).toBe(1);
});

// The switch changes while the room's first load still waits for the server.
// The load for the old value is not reused, so the upgrade reads are made.
test('a switch change during the first load still loads the upgrade reads', async () => {
  let upgrades = false;
  let history = 0;
  let answerVersion: (() => void) | undefined;
  const ctx = appContext(
    {
      'GET /api/v1/ui': () => json(200, { upgrades }),
      'GET /api/v1/server/version': () =>
        new Promise((resolve) => {
          answerVersion = () => {
            resolve(json(200, { version: '1.2.0' }));
          };
        }),
      'GET /api/v1/server/upgrade/history': () => {
        history += 1;
        return json(200, []);
      },
    },
    { path: PATHS.settingsServerOverview },
  );
  await ctx.settings.read();
  await render(Server, { props: { ctx } });

  upgrades = true;
  await ctx.settings.read();
  answerVersion?.();

  await vi.waitFor(() => {
    expect(history).toBe(1);
  });
});
