// The first run when the library's wizard chunk does not load, as after a new
// build of the server replaced it.
import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import FirstRun from './first-run.svelte';

describe('the first run', () => {
  it('says the setup could not be opened and offers a reload when its code does not load', async () => {
    await render(FirstRun, {
      props: {
        // The wizard is never drawn, so its own props are never read.
        ...({} as Omit<Parameters<typeof FirstRun>[1], 'load'>),
        load: () => Promise.reject(new Error('Failed to fetch module')),
      },
    });

    await expect
      .element(
        page.getByRole('alert').getByRole('heading', {
          level: 1,
          name: 'The setup could not be opened',
        }),
      )
      .toBeVisible();
    await expect
      .element(page.getByRole('button', { name: 'Reload the page' }))
      .toBeVisible();
  });
});
