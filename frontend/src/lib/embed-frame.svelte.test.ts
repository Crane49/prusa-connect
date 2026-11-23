import { theme } from '@prusa/connect-ui';
import { createRawSnippet, flushSync } from 'svelte';
import { afterEach, describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import EmbedFrame from './embed-frame.svelte';

const children = createRawSnippet(() => ({
  render: () => '<p>the module page</p>',
}));

afterEach(() => {
  theme.follow('system');
  document.documentElement.classList.remove('dark');
});

describe('the embed frame', () => {
  it('draws the page alone, with no navigation', async () => {
    await render(EmbedFrame, { props: { children } });

    await expect
      .element(page.getByRole('main'))
      .toHaveTextContent('the module page');
    await expect.element(page.getByRole('navigation')).not.toBeInTheDocument();
  });

  // The app shell sets the theme on every other page. Without it here an
  // embedded page would stay light under a dark slicer.
  it('sets the theme on the document', async () => {
    theme.follow('light');
    await render(EmbedFrame, { props: { children } });
    expect(document.documentElement.classList.contains('dark')).toBe(false);

    theme.follow('dark');
    flushSync();

    expect(document.documentElement.classList.contains('dark')).toBe(true);
  });
});
