import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import AppFooter from './app-footer.svelte';

describe('AppFooter', () => {
  it('leads to the API reference the server serves', async () => {
    await render(AppFooter, { props: { apiReference: '/docs' } });

    await expect
      .element(page.getByRole('link', { name: 'API' }))
      .toHaveAttribute('href', '/docs');
  });

  // The printer build leaves the reference page out, and a link there would 404.
  it('offers no API link where the server serves no reference', async () => {
    await render(AppFooter, { props: {} });

    expect(page.getByRole('link', { name: 'API' }).elements()).toHaveLength(0);
  });

  it('sends support to the beta assistant and links no repository', async () => {
    await render(AppFooter, { props: {} });

    await expect
      .element(page.getByRole('link', { name: 'Support' }))
      .toHaveAttribute(
        'href',
        'https://beta.prusa3d.com/connect-local/assistant',
      );
    expect(
      page.getByRole('link', { name: 'Repository' }).elements(),
    ).toHaveLength(0);
  });
});
