import InboxIcon from '@lucide/svelte/icons/inbox';
import { createRawSnippet } from 'svelte';
import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import EmptyState from './empty-state.svelte';
import type { EmptyStateHeadingLevel } from './types';

const base = {
  heading: 'No printers yet',
  headingLevel: 'h2' as EmptyStateHeadingLevel,
};

describe('EmptyState', () => {
  it('draws the heading at the level the caller asked for', async () => {
    const levels: EmptyStateHeadingLevel[] = [
      'h1',
      'h2',
      'h3',
      'h4',
      'h5',
      'h6',
    ];

    for (const headingLevel of levels) {
      const { container, unmount } = await render(EmptyState, {
        props: { ...base, headingLevel },
      });

      const heading = container.querySelector('h1, h2, h3, h4, h5, h6');
      if (heading === null) throw new Error(`no ${headingLevel} rendered`);

      expect(heading.tagName.toLowerCase()).toBe(headingLevel);
      expect(heading.textContent.trim()).toBe('No printers yet');
      await unmount();
    }
  });

  it('leaves out the description and the action a caller does not give', async () => {
    const { container } = await render(EmptyState, { props: base });

    expect(container.querySelector('p')).toBeNull();
    expect(container.querySelectorAll('div').length).toBe(1);
  });

  it('draws the description a caller gives', async () => {
    const { container } = await render(EmptyState, {
      props: { ...base, description: 'Printers you add will show up here.' },
    });

    expect(container.querySelector('p')?.textContent).toBe(
      'Printers you add will show up here.',
    );
  });

  it('draws the action a caller gives', async () => {
    await render(EmptyState, {
      props: {
        ...base,
        description: 'Printers you add will show up here.',
        action: createRawSnippet(() => ({
          render: () => '<button type="button">Add a printer</button>',
        })),
      },
    });

    await expect
      .element(page.getByRole('button', { name: 'Add a printer' }))
      .toBeInTheDocument();
  });

  // The drawing repeats the heading, so a screen reader that read it would say
  // the same thing twice.
  it('hides the drawing from the accessibility tree', async () => {
    const { container } = await render(EmptyState, {
      props: { ...base, icon: InboxIcon },
    });

    expect(container.querySelector('svg')?.getAttribute('aria-hidden')).toBe(
      'true',
    );
  });

  it('has nothing axe refuses', async () => {
    const { container } = await render(EmptyState, {
      props: {
        ...base,
        icon: InboxIcon,
        description: 'Printers you add will show up here.',
      },
    });

    await expectNoViolations(container);
  });
});

describe('EmptyState small heading', () => {
  it('is an h3 when a card-sized message names no level', async () => {
    await render(EmptyState, {
      props: { heading: 'Nothing here', size: 'sm' },
    });
    expect(document.querySelector('h3')?.textContent.trim()).toBe(
      'Nothing here',
    );
  });
});
