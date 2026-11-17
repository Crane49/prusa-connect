import { createRawSnippet } from 'svelte';
import { describe, expect, it, vi } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import DataRow from './data-row.svelte';

const actionsMenu = createRawSnippet(() => ({
  render: () => '<button type="button" aria-label="Actions">…</button>',
}));

const leadingGlyph = createRawSnippet(() => ({
  render: () => '<button type="button" aria-label="Expand">+</button>',
}));

/** The centre of an element, which is where a pointer would land on it. */
const hitAtCentre = (element: Element): Element | null => {
  const box = element.getBoundingClientRect();
  return document.elementFromPoint(
    box.left + box.width / 2,
    box.top + box.height / 2,
  );
};

describe('DataRow', () => {
  // The whole point of the row: a press anywhere on it opens what it is
  // about, through the one link the row already has.
  it('stretches the primary link over the whole row', async () => {
    await render(DataRow, {
      props: {
        primary: 'benchy.3mf',
        href: '#benchy',
        secondaryValues: ['4.1 MB', '2026-08-14'],
      },
    });

    const link = document.querySelector('a');
    const value = page.getByText('4.1 MB').element();
    expect(hitAtCentre(value)).toBe(link);
  });

  // A row that is `<a><button></button></a>` is invalid, so the actions sit
  // beside the link and are lifted above its overlay. Without the lift the
  // overlay swallows every press on them.
  it('keeps the actions above the overlay', async () => {
    await render(DataRow, {
      props: { primary: 'benchy.3mf', href: '#benchy', actions: actionsMenu },
    });

    const trigger = page.getByRole('button', { name: 'Actions' }).element();
    expect(hitAtCentre(trigger)).toBe(trigger);
  });

  it('keeps a button in the leading slot clickable', async () => {
    await render(DataRow, {
      props: { primary: 'brackets', href: '#brackets', leading: leadingGlyph },
    });

    const expand = page.getByRole('button', { name: 'Expand' }).element();
    expect(hitAtCentre(expand)).toBe(expand);
  });

  it('reaches the link and then the actions by keyboard', async () => {
    await render(DataRow, {
      props: { primary: 'benchy.3mf', href: '#benchy', actions: actionsMenu },
    });

    await userEvent.tab();
    await expect
      .element(page.getByRole('link', { name: 'benchy.3mf' }))
      .toHaveFocus();

    await userEvent.tab();
    await expect
      .element(page.getByRole('button', { name: 'Actions' }))
      .toHaveFocus();
  });

  it('activates in place without a link', async () => {
    const onActivate = vi.fn();
    await render(DataRow, { props: { primary: 'production', onActivate } });

    await page.getByRole('button', { name: 'production' }).click();

    expect(onActivate).toHaveBeenCalledTimes(1);
    expect(document.querySelectorAll('a')).toHaveLength(0);
  });

  // A control that is not a disclosure must not claim `aria-expanded`. A
  // selection toggle reporting "collapsed" tells a screen reader user there
  // is something to open.
  it('reports aria-expanded only for a disclosure', async () => {
    const slot = '[data-slot="data-row-activate"]';

    const toggle = await render(DataRow, {
      props: { primary: 'benchy.3mf', onActivate: vi.fn() },
    });
    expect(document.querySelector(slot)?.hasAttribute('aria-expanded')).toBe(
      false,
    );
    await toggle.unmount();

    await render(DataRow, {
      props: {
        primary: 'production',
        onActivate: vi.fn(),
        activateExpanded: false,
      },
    });
    expect(document.querySelector(slot)?.getAttribute('aria-expanded')).toBe(
      'false',
    );
  });

  it('draws no link and no button when the row goes nowhere', async () => {
    await render(DataRow, { props: { primary: 'benchy.3mf' } });

    expect(document.querySelectorAll('a')).toHaveLength(0);
    expect(document.querySelectorAll('button')).toHaveLength(0);
    expect(
      document
        .querySelector('[data-slot="data-row"]')
        ?.className.includes('hover:bg-accent'),
    ).toBe(false);
  });

  it('drops a value a caller left out and closes the gap', async () => {
    await render(DataRow, {
      props: {
        primary: 'bracket_v2.stl',
        secondaryValues: [false, '820 KB', undefined, null, '2026-08-14'],
      },
    });

    const cells = document.querySelectorAll(
      '[data-slot="data-row"] .truncate.min-w-0',
    );
    expect([...cells].map((cell) => cell.textContent.trim())).toEqual([
      '820 KB',
      '2026-08-14',
    ]);
  });

  // The third value onward is the row's own width query. Wrapping shows it
  // at any width instead.
  it('holds a third value back until the row is wide, unless it wraps', async () => {
    const values = ['Ready', '4.1 MB', '2026-08-14'];

    const narrow = await render(DataRow, {
      props: { primary: 'benchy.3mf', secondaryValues: values },
    });
    const third = () =>
      [
        ...document.querySelectorAll(
          '[data-slot="data-row"] > div > div > span',
        ),
      ].at(-1)?.className ?? '';
    expect(third()).toContain('hidden');
    expect(third()).toContain('@[40rem]/data-row:inline-flex');
    await narrow.unmount();

    await render(DataRow, {
      props: {
        primary: 'benchy.3mf',
        secondaryValues: values,
        wrapValues: true,
      },
    });
    expect(third()).not.toContain('hidden');
    expect(third()).toContain('inline-flex');
  });

  it('draws the name in the value face when it is a file name', async () => {
    await render(DataRow, {
      props: { primary: 'benchy.3mf', mono: true, href: '#benchy' },
    });

    expect(document.querySelector('a')?.className).toContain('font-mono');
  });

  it('has nothing axe objects to', async () => {
    const { container } = await render(DataRow, {
      props: {
        primary: 'benchy.3mf',
        href: '#benchy',
        secondaryValues: ['4.1 MB', '2026-08-14'],
        actions: actionsMenu,
      },
    });

    await expectNoViolations(container);
  });
});
