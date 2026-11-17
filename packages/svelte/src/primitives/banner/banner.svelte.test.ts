import { createRawSnippet } from 'svelte';
import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import Banner from './banner.svelte';

const text = (value: string) =>
  createRawSnippet(() => ({ render: () => `<span>${value}</span>` }));

describe('Banner', () => {
  it('announces itself as a status carrying its message and actions', async () => {
    const { container } = await render(Banner, {
      props: {
        children: text('Safe mode'),
        actions: createRawSnippet(() => ({
          render: () => '<button>Restart</button>',
        })),
      },
    });

    await expect
      .element(page.getByRole('status'))
      .toHaveTextContent('Safe mode Restart');
    await expect
      .element(page.getByRole('button', { name: 'Restart' }))
      .toBeVisible();
    await expectNoViolations(container);
  });
});
