import { describe, expect, it } from 'vitest';
import { page, userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import ListingSkeleton from './listing-skeleton.svelte';
import type { ListingSkeletonProps } from './types';

const draw = async (props: Partial<ListingSkeletonProps> = {}) => {
  const result = await render(ListingSkeleton, {
    props: { label: 'Loading jobs', ...props },
  });
  const box = result.container.querySelector<HTMLElement>('[role="status"]');
  if (box === null) throw new Error('no listing skeleton rendered');
  return { ...result, box };
};

describe('ListingSkeleton', () => {
  it('says what is loading, which is all a screen reader gets', async () => {
    const { box } = await draw();

    await expect
      .element(page.getByRole('status', { name: 'Loading jobs' }))
      .toBeVisible();
    expect(box.textContent.trim()).toBe('');
  });

  it('shows five rows unless the caller asks for another number', async () => {
    const { box, unmount } = await draw();
    expect(box.children.length).toBe(5);
    await unmount();

    const shorter = await draw({ rows: 2 });
    expect(shorter.box.children.length).toBe(2);
  });

  it('gives every row two bars, a wide one and a narrow one', async () => {
    const { box } = await draw({ rows: 1 });
    const bars = box.querySelectorAll('[data-slot="skeleton"]');

    expect(bars.length).toBe(2);
    expect(bars[0]?.classList.contains('flex-1')).toBe(true);
    expect(bars[1]?.classList.contains('w-24')).toBe(true);
  });

  it('leads a row with a square when the listing has row art', async () => {
    const { box, unmount } = await draw({ rows: 1 });
    expect(box.querySelector('.size-6')).toBeNull();
    await unmount();

    const withArt = await draw({ rows: 1, art: true });
    const bars = withArt.box.querySelectorAll('[data-slot="skeleton"]');

    expect(bars.length).toBe(3);
    expect(bars[0]?.classList.contains('size-6')).toBe(true);
    expect(bars[0]?.classList.contains('shrink-0')).toBe(true);
  });

  it('takes no focus, so a keyboard walk goes straight past it', async () => {
    const { container, box } = await draw();

    await userEvent.tab();

    expect(box.contains(document.activeElement)).toBe(false);
    expect(container.querySelector('a, button, input, [tabindex]')).toBeNull();
  });

  it('keeps the class a caller adds beside its own', async () => {
    const { box } = await draw({ class: 'px-4' });

    expect(box.classList.contains('px-4')).toBe(true);
    expect(box.classList.contains('flex-col')).toBe(true);
  });

  it('has no accessibility violation', async () => {
    const { container } = await draw({ art: true });

    await expectNoViolations(container);
  });
});
