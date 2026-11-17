import { cdp } from 'vitest/browser';
import { afterEach, describe, expect, it } from 'vitest';
import { render } from 'vitest-browser-svelte';

import Spinner from './spinner.svelte';

const emulate = (reducedMotion: 'reduce' | 'no-preference') =>
  cdp().send('Emulation.setEmulatedMedia', {
    features: [{ name: 'prefers-reduced-motion', value: reducedMotion }],
  });

const animationName = (container: HTMLElement) =>
  getComputedStyle(container.querySelector('[data-slot="spinner"]')!)
    .animationName;

describe('Spinner', () => {
  afterEach(() => emulate('no-preference'));

  it('turns, and is hidden from assistive technology', async () => {
    await emulate('no-preference');
    const { container } = await render(Spinner, {});

    expect(animationName(container)).not.toBe('none');
    expect(
      container
        .querySelector('[data-slot="spinner"]')
        ?.getAttribute('aria-hidden'),
    ).toBe('true');
  });

  it('stands still under reduced motion', async () => {
    await emulate('reduce');
    const { container } = await render(Spinner, {});

    expect(animationName(container)).toBe('none');
  });
});
