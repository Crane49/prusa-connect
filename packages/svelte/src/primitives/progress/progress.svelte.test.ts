import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import Progress from './progress.svelte';

const indicator = (container: Element): HTMLElement => {
  const found = container.querySelector<HTMLElement>(
    '[data-slot="progress-indicator"]',
  );
  if (found === null) throw new Error('no indicator rendered');
  return found;
};

/**
 * How much of the track is still empty, as a fraction of the whole: -1 when
 * the bar is at zero and 0 when it is full. Read off the computed transform,
 * which is where the fill is positioned.
 */
const emptyFraction = (container: Element): number => {
  const box = indicator(container);
  const matrix = new DOMMatrixReadOnly(getComputedStyle(box).transform);
  return matrix.m41 / box.getBoundingClientRect().width;
};

describe('Progress', () => {
  it('reports its reading to a screen reader', async () => {
    await render(Progress, {
      props: { value: 42, 'aria-label': 'Print progress' },
    });

    const bar = page.getByRole('progressbar', { name: 'Print progress' });
    await expect.element(bar).toHaveAttribute('aria-valuenow', '42');
    await expect.element(bar).toHaveAttribute('aria-valuemin', '0');
    await expect.element(bar).toHaveAttribute('aria-valuemax', '100');
    await expect.element(bar).toHaveAttribute('data-state', 'loading');
  });

  it('fills further for a larger reading', async () => {
    const quarter = await render(Progress, {
      props: { value: 25, 'aria-label': 'Print progress' },
    });
    expect(emptyFraction(quarter.container)).toBeCloseTo(-0.75, 2);
    await quarter.unmount();

    const most = await render(Progress, {
      props: { value: 75, 'aria-label': 'Print progress' },
    });
    expect(emptyFraction(most.container)).toBeCloseTo(-0.25, 2);
  });

  it('reads the scale it is given, not a hundred', async () => {
    const { container } = await render(Progress, {
      props: { value: 5, max: 10, 'aria-label': 'Print progress' },
    });

    // Half of ten is half the track, whatever the maximum happens to be.
    expect(emptyFraction(container)).toBeCloseTo(-0.5, 2);
  });

  // A screen reader announcing "0%" for unknown progress is worse than
  // silence, and an empty bar is worse still: nobody can tell it from 0%.
  it('fills its track and says no number while it has no reading', async () => {
    const { container } = await render(Progress, {
      props: { value: null, 'aria-label': 'Print progress' },
    });

    const bar = page.getByRole('progressbar', { name: 'Print progress' });
    expect(bar.element().getAttribute('aria-valuenow')).toBeNull();
    await expect.element(bar).toHaveAttribute('aria-valuemin', '0');
    await expect.element(bar).toHaveAttribute('aria-valuemax', '100');
    await expect.element(bar).toHaveAttribute('data-state', 'indeterminate');

    const fill = indicator(container);
    expect(fill.style.transform).toBe('');
    expect(fill.getBoundingClientRect().width).toBeCloseTo(
      bar.element().getBoundingClientRect().width,
      1,
    );
    // Grey and not the tone, so a full bar with no reading is not read as done.
    expect(fill.classList.contains('bg-primary')).toBe(false);
    expect(fill.classList.contains('bg-muted-foreground/40')).toBe(true);
  });

  it('renders each tone with its own fill', async () => {
    const tones = ['primary', 'muted', 'success'] as const;
    const fills = new Set<string>();

    for (const tone of tones) {
      const { container, unmount } = await render(Progress, {
        props: { tone, value: 50, 'aria-label': 'Print progress' },
      });
      fills.add(getComputedStyle(indicator(container)).backgroundColor);
      await unmount();
    }

    expect(fills.size).toBe(tones.length);
  });

  it('has nothing axe objects to', async () => {
    const { container } = await render(Progress, {
      props: { value: 42, 'aria-label': 'Print progress' },
    });

    await expectNoViolations(container);
  });
});
