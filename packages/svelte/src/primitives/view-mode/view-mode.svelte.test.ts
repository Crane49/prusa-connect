import { describe, expect, it } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import ViewModeProbe from '../../test/view-mode-probe.svelte';
import { ViewModeState } from './choice.svelte';

// Every item is a tooltip trigger as well as a toggle item, and the trigger's
// own `data-slot` is the one that reaches the element, so the items are found
// by the role bits-ui gives them.
/** The name each item carries, in the order the group draws them. */
const names = (): (string | null)[] =>
  [...document.querySelectorAll('[role="radio"]')].map((item) =>
    item.getAttribute('aria-label'),
  );

describe('ViewModeToggle', () => {
  it('names the group and puts the views in the canonical order', async () => {
    const { container } = await render(ViewModeProbe, {
      props: { modes: ['frames', 'cards', 'rows', 'table'] },
    });

    await expect
      .element(page.getByRole('group', { name: 'Display mode' }))
      .toBeInTheDocument();
    // Asked for backwards, drawn dense first and the cameras last, so a
    // screen cannot put its own views in its own order by accident.
    expect(names()).toEqual(['Table', 'Rows', 'Cards', 'Cameras']);
    expect(container.querySelectorAll('[role="radio"]')).toHaveLength(4);
  });

  it('fills the chosen view and moves the fill on a click', async () => {
    await render(ViewModeProbe, {});

    const table = page.getByRole('radio', { name: 'Table' });
    const rows = page.getByRole('radio', { name: 'Rows' });

    await expect.element(table).toHaveAttribute('aria-checked', 'true');
    const fill = (element: Element) =>
      getComputedStyle(element).backgroundColor;
    const resting = fill(rows.element());
    const chosen = fill(table.element());

    expect(chosen).not.toBe(resting);
    // A real fill and not a hairline. Every item is also a tooltip trigger,
    // which writes its own `data-state` over the toggle's, so the fill hangs
    // off `aria-checked` instead.
    expect(chosen).not.toContain('/ 0)');

    await rows.click();

    await expect.element(rows).toHaveAttribute('aria-checked', 'true');
    await expect.element(page.getByTestId('chosen')).toHaveTextContent('rows');
    // The fill is a transition, so this waits for the paint rather than
    // reading the frame the click returned in.
    await expect.poll(() => fill(table.element())).toBe(resting);
    expect(fill(rows.element())).not.toBe(resting);
  });

  it('keeps a view that does not fit reachable and says why', async () => {
    await render(ViewModeProbe, {
      props: { initial: 'rows', unavailable: 'table' },
    });

    const table = page.getByRole('radio', { name: 'Table' });

    await expect.element(table).toHaveAttribute('aria-disabled', 'true');
    // Not `disabled`: that would drop it out of the tab order and take its
    // own tooltip with it, which is where the reason is written.
    expect(table.element().hasAttribute('disabled')).toBe(false);
    expect(
      (table.element() as HTMLButtonElement).tabIndex,
    ).toBeGreaterThanOrEqual(0);

    // The pointer still rests where the last test clicked, over Rows. A key
    // press makes Chromium apply that hover to the fresh render, and Rows'
    // tooltip would replace the one under test.
    await userEvent.unhover(page.getByRole('radio', { name: 'Rows' }));

    // From the keyboard: a tooltip opens on keyboard focus alone.
    await userEvent.tab();
    await expect.element(table).toHaveFocus();
    await expect
      .element(page.getByRole('tooltip'))
      .toHaveTextContent('It needs a wider window.');
  });

  it('changes nothing when a view that does not fit is clicked', async () => {
    await render(ViewModeProbe, {
      props: { initial: 'rows', unavailable: 'table' },
    });

    // `force`, because the driver refuses to press a control that reports
    // `aria-disabled`, and the point of this test is what the press does.
    await page.getByRole('radio', { name: 'Table' }).click({ force: true });

    await expect.element(page.getByTestId('chosen')).toHaveTextContent('rows');
    // And the view the reader is in keeps its own mark, rather than the group
    // ending up with nothing chosen at all.
    await expect
      .element(page.getByRole('radio', { name: 'Rows' }))
      .toHaveAttribute('aria-checked', 'true');
  });

  it('names an icon-only button in a tooltip', async () => {
    await render(ViewModeProbe, {});

    await userEvent.tab();
    await expect.element(page.getByRole('tooltip')).toHaveTextContent('Table');
  });

  it('has nothing axe refuses', async () => {
    const { container } = await render(ViewModeProbe, {
      props: { modes: ['table', 'rows', 'cards'], unavailable: 'cards' },
    });

    await expectNoViolations(container);
  });
});

describe('ViewModeState', () => {
  /** A container of a fixed width, gone again when the test ends. */
  const column = (width: string): HTMLElement => {
    const box = document.createElement('div');
    box.style.width = width;
    document.body.append(box);

    return box;
  };

  it('remembers a choice the container cannot hold, and gives it back', async () => {
    const box = column('20rem');
    const fit = new ViewModeState({
      initial: 'table',
      fallback: 'rows',
      minRem: { table: 40 },
    });
    const stop = fit.measure(box);

    // 20rem of container against a 40rem threshold.
    await expect.poll(() => fit.mode).toBe('rows');
    expect(fit.chosen).toBe('table');
    expect(fit.roomFor('table')).toBe(false);
    // A view with no threshold of its own always fits.
    expect(fit.roomFor('rows')).toBe(true);

    box.style.width = '48rem';

    await expect.poll(() => fit.mode).toBe('table');

    stop();
    box.remove();
  });

  it('holds a view that fits until the container says otherwise', async () => {
    const box = column('48rem');
    const fit = new ViewModeState({
      initial: 'table',
      fallback: 'rows',
      minRem: { table: 40 },
    });
    const stop = fit.measure(box);

    await expect.poll(() => fit.roomFor('table')).toBe(true);
    expect(fit.mode).toBe('table');

    fit.chosen = 'rows';
    expect(fit.mode).toBe('rows');

    stop();
    box.remove();
  });
});
