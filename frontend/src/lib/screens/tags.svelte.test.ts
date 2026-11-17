// The tags page reads the tags off the fleet's own records, and a tag leads
// to the printers list filtered to it.
import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { appContext } from '../../test/app-context';
import { json, type Routes } from '../../test/stub';
import { printersTaggedHref } from '../routes';
import Tags from './tags.svelte';

const FLEET: Routes = {
  'GET /api/v1/devices': json(200, {
    items: [
      {
        id: 'a',
        name: 'Alpha',
        kind: 'printer',
        labels: {},
        tags: ['workshop'],
        state: { '/state': 'IDLE' },
      },
      {
        id: 'b',
        name: 'Beta',
        kind: 'printer',
        labels: {},
        tags: ['workshop', 'night shift'],
        state: { '/state': 'IDLE' },
      },
    ],
    total: 2,
    limit: 50,
    offset: 0,
    issues: { affected: 0, counts: {} },
  }),
};

describe('the tags page', () => {
  it('lists a tag the fleet carries with the printers carrying it', async () => {
    await render(Tags, { props: { ctx: appContext(FLEET) } });

    await expect.element(page.getByText('night shift')).toBeInTheDocument();
    await expect
      .element(page.getByRole('button', { name: 'On 2 printers' }))
      .toBeInTheDocument();
  });

  it('links to the printers list filtered to the tag', async () => {
    await render(Tags, { props: { ctx: appContext(FLEET) } });

    await expect
      .element(
        page.getByRole('link', {
          name: 'Show the printers tagged night shift',
        }),
      )
      .toHaveAttribute('href', printersTaggedHref(['night shift']));
  });
});
