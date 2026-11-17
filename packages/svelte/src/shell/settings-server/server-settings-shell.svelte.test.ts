import { createRawSnippet } from 'svelte';
import { expect, test } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import ServerSettingsShell from './server-settings-shell.svelte';

const children = createRawSnippet(() => ({
  render: () => '<div data-testid="content"></div>',
}));

test('marks the route-driven tab, with Overview first', async () => {
  await render(ServerSettingsShell, {
    props: {
      active: 'backups',
      overviewHref: '/settings/server/overview',
      healthHref: '/settings/server/health',
      updatesHref: '/settings/server/updates',
      backupsHref: '/settings/server/backups',
      networkHref: '/settings/server/network',
      children,
    },
  });

  await expect
    .element(page.getByRole('link', { name: 'Backups' }))
    .toHaveAttribute('aria-current', 'page');
  await expect
    .element(page.getByRole('link').first())
    .toHaveAccessibleName('Overview');
  await expect.element(page.getByTestId('content')).toBeInTheDocument();
});

test('draws no Updates and no Backups tab where the server prepares no upgrade', async () => {
  await render(ServerSettingsShell, {
    props: {
      active: 'overview',
      overviewHref: '/settings/server/overview',
      healthHref: '/settings/server/health',
      networkHref: '/settings/server/network',
      children,
    },
  });

  // A tab drawn without an address is not a link, so the tabs are read by
  // their text.
  const tabs = page.getByRole('navigation', { name: 'Server settings' });
  await expect
    .element(tabs.getByText('Network', { exact: true }))
    .toBeVisible();
  expect(tabs.getByText('Updates', { exact: true }).elements()).toEqual([]);
  expect(tabs.getByText('Backups', { exact: true }).elements()).toEqual([]);
  await expect
    .element(
      page.getByText(
        'Whether this server is ready to work, and how it appears on the local network.',
      ),
    )
    .toBeVisible();
});
