// The dashboards route when the library's dashboard chunk does not load, as
// after a new build of the server replaced it.
import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { appContext } from '../../test/app-context';
import { json } from '../../test/stub';
import DashboardPage from './dashboard.svelte';

describe('the dashboards route', () => {
  it('says the dashboard could not be opened and offers a reload when its code does not load', async () => {
    const ctx = appContext({ 'GET /api/v1/devices': json(200, { items: [] }) });
    await render(DashboardPage, {
      props: {
        ctx,
        load: () => Promise.reject(new Error('Failed to fetch module')),
      },
    });

    await expect
      .element(
        page.getByRole('alert').getByRole('heading', {
          level: 1,
          name: 'The dashboard could not be opened',
        }),
      )
      .toBeVisible();
    await expect
      .element(page.getByRole('button', { name: 'Reload the page' }))
      .toBeVisible();
  });
});
