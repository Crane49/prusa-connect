import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import RequestList from './request-list.svelte';

describe('RequestList data folder', () => {
  it('names the data folder by the manifest name and says Remove keeps it', async () => {
    await render(RequestList, {
      props: {
        requests: [{ kind: 'data' }],
        instance: 'buddy-debug-logs',
        label: 'This extension asks for:',
      },
    });

    const line = page.getByText(/module-data\/buddy-debug-logs,? .*Removing/);
    await expect.element(line).toBeVisible();
    await expect
      .element(page.getByText(/com\.github\.prusa3d/))
      .not.toBeInTheDocument();
  });
});
