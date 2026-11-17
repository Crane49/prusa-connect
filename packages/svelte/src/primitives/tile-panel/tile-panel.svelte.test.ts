import { createRawSnippet } from 'svelte';
import { describe, expect, it } from 'vitest';
import { render } from 'vitest-browser-svelte';

import TilePanel from './tile-panel.svelte';

const children = createRawSnippet(() => ({ render: () => '<p>Body</p>' }));

describe('TilePanel', () => {
  it('shows the fallback when the title is blank', async () => {
    const { container } = await render(TilePanel, {
      props: { title: '  ', fallback: 'Clock', children },
    });
    expect(container.querySelector('h2')?.textContent).toBe('Clock');
  });

  it('draws no heading row without a heading or trailing', async () => {
    const { container } = await render(TilePanel, {
      props: { title: '', children },
    });
    expect(container.querySelector('h2')).toBeNull();
  });
});
