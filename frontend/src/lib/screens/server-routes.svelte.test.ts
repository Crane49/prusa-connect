import { expect, test, vi } from 'vitest';
import { render } from 'vitest-browser-svelte';

import { appContext } from '../../test/app-context';
import { json } from '../../test/stub';
import { PATHS } from '../routes';
import MaintenanceAddress from './maintenance.svelte';
import OldNetworkAddress from './old-network-address.svelte';
import ServerAddress from './server-address.svelte';

test('the Server address resolves to Overview with replace semantics', async () => {
  const go = vi.fn();
  await render(ServerAddress, {
    props: { ctx: appContext({}, { path: PATHS.settingsServer, go }) },
  });

  expect(go).toHaveBeenCalledWith(PATHS.settingsServerOverview, {
    replace: true,
  });
});

test('the old Maintenance address resolves to Updates with replace semantics', async () => {
  const go = vi.fn();
  await render(MaintenanceAddress, {
    props: { ctx: appContext({}, { path: PATHS.settingsMaintenance, go }) },
  });

  expect(go).toHaveBeenCalledWith(PATHS.settingsServerUpdates, {
    replace: true,
  });
});

// core.upgrade.enabled off: Updates is not drawn, so the old address leads
// to the overview rather than to a page this server does not answer.
test('the old Maintenance address resolves to Overview where upgrades are off', async () => {
  const go = vi.fn();
  const ctx = appContext(
    { 'GET /api/v1/ui': () => json(200, { upgrades: false }) },
    { path: PATHS.settingsMaintenance, go },
  );
  await ctx.settings.read();
  await render(MaintenanceAddress, { props: { ctx } });

  expect(go).toHaveBeenCalledWith(PATHS.settingsServerOverview, {
    replace: true,
  });
  expect(go).not.toHaveBeenCalledWith(
    PATHS.settingsServerUpdates,
    expect.anything(),
  );
});

test('the old Network address resolves to the Network tab with replace semantics', async () => {
  const go = vi.fn();
  await render(OldNetworkAddress, {
    props: { ctx: appContext({}, { path: PATHS.settingsOldNetwork, go }) },
  });

  expect(go).toHaveBeenCalledWith(PATHS.settingsServerNetwork, {
    replace: true,
  });
});
