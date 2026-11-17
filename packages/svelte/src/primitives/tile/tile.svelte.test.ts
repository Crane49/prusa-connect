import { createRawSnippet, mount, unmount } from 'svelte';
import { describe, expect, it, vi } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import Checkbox from '../checkbox/checkbox.svelte';
import Tile from './tile.svelte';

const content = (text: string) =>
  createRawSnippet(() => ({ render: () => `<span>${text}</span>` }));

const checkbox = createRawSnippet(() => ({
  render: () => '<span></span>',
  setup: (target) => {
    const box = mount(Checkbox, {
      target,
      props: { 'aria-label': 'Select benchy.gcode' },
    });
    return () => {
      void unmount(box);
    };
  },
}));

describe('Tile', () => {
  it('presses its button on Enter and says it is pressed', async () => {
    const onclick = vi.fn();
    await render(Tile, {
      props: { onclick, pressed: true, children: content('Bed 1') },
    });

    const button = page.getByRole('button', { name: 'Bed 1' });
    await expect.element(button).toHaveAttribute('aria-pressed', 'true');

    await userEvent.tab();
    await expect.element(button).toHaveFocus();
    await userEvent.keyboard('{Enter}');
    expect(onclick).toHaveBeenCalledTimes(1);
  });

  it('draws a link when it is given an href', async () => {
    await render(Tile, {
      props: { href: '/storage/prints', children: content('prints') },
    });

    await expect
      .element(page.getByRole('link', { name: 'prints' }))
      .toHaveAttribute('href', '/storage/prints');
  });

  // The press element is stretched over the whole tile; the checkbox has to
  // sit above it or every tick would also open the tile.
  it('ticks the checkbox in top without pressing the tile', async () => {
    const onclick = vi.fn();
    await render(Tile, {
      props: { onclick, top: checkbox, children: content('benchy.gcode') },
    });

    const box = page.getByRole('checkbox', { name: 'Select benchy.gcode' });
    await box.click();
    await expect.element(box).toBeChecked();
    expect(onclick).not.toHaveBeenCalled();
  });

  it('has nothing axe refuses', async () => {
    const { container } = await render(Tile, {
      props: { onclick: vi.fn(), top: checkbox, children: content('Bed 1') },
    });

    await expectNoViolations(container);
  });
});
