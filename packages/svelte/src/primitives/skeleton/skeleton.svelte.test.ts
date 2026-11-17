import { describe, expect, it } from 'vitest';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import Skeleton from './skeleton.svelte';

const draw = async (props = {}): Promise<HTMLElement> => {
  const { container } = await render(Skeleton, { props });
  const block = container.querySelector<HTMLElement>('[data-slot="skeleton"]');
  if (block === null) throw new Error('no skeleton rendered');
  return block;
};

describe('Skeleton', () => {
  it('pulses, which is what says the page is still working', async () => {
    const block = await draw();
    const style = getComputedStyle(block);

    expect(style.animationName).not.toBe('none');
    expect(Number.parseFloat(style.animationDuration)).toBeGreaterThan(0);
    expect(style.animationIterationCount).toBe('infinite');
  });

  it('takes the size the caller gives it', async () => {
    const block = await draw({ class: 'size-10' });
    const box = block.getBoundingClientRect();

    expect(box.width).toBeGreaterThan(0);
    expect(box.width).toBe(box.height);
  });

  it('carries the attributes the caller sets', async () => {
    const block = await draw({ 'aria-hidden': 'true', id: 'title-line' });

    expect(block.getAttribute('aria-hidden')).toBe('true');
    expect(block.id).toBe('title-line');
  });

  it('says nothing a screen reader reads out', async () => {
    const block = await draw({ class: 'h-4 w-48' });

    expect(block.getAttribute('role')).toBeNull();
    expect(block.textContent).toBe('');
    await expectNoViolations(block);
  });
});
