import { describe, expect, it } from 'vitest';
import { userEvent } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import ScrollAreaProbe from '../../test/scroll-area-probe.svelte';

const lines = Array.from(
  { length: 40 },
  (_, index) => `[00:00:${String(index).padStart(2, '0')}] heater target`,
);

const viewport = (): HTMLElement => {
  const found = document.querySelector<HTMLElement>(
    '[data-slot="scroll-area-viewport"]',
  );
  if (found === null) throw new Error('no viewport rendered');
  return found;
};

const scrollbar = (orientation: string): HTMLElement => {
  const found = document.querySelector<HTMLElement>(
    `[data-slot="scroll-area-scrollbar"][data-orientation="${orientation}"]`,
  );
  if (found === null) throw new Error(`no ${orientation} scrollbar rendered`);
  return found;
};

describe('ScrollArea', () => {
  it('overflows its box rather than growing it', async () => {
    await render(ScrollAreaProbe, { props: { lines } });

    expect(viewport().scrollHeight).toBeGreaterThan(viewport().clientHeight);
    expect(getComputedStyle(viewport()).overflowY).toBe('scroll');
  });

  it('takes the focus one Tab in, and scrolls from the keyboard', async () => {
    await render(ScrollAreaProbe, { props: { lines } });

    await userEvent.tab();
    expect(document.activeElement).toBe(viewport());

    // A real key press, so the browser performs its own scroll. A synthetic
    // event is refused for this: scrolling on keydown is a trusted-event
    // action.
    await userEvent.keyboard('{PageDown}');
    await expect.poll(() => viewport().scrollTop).toBeGreaterThan(0);
  });

  it('draws a bar for each axis, each one along its own edge', async () => {
    await render(ScrollAreaProbe, { props: { lines, type: 'always' } });

    expect(scrollbar('vertical').getAttribute('class')).toContain('w-2.5');
    expect(scrollbar('horizontal').getAttribute('class')).toContain('h-2.5');
    expect(scrollbar('vertical').getAttribute('class')).not.toContain(
      'flex-col',
    );
  });

  it('has nothing axe refuses', async () => {
    const { container } = await render(ScrollAreaProbe, { props: { lines } });

    await expectNoViolations(container);
  });
});
