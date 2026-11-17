import { afterEach, describe, expect, it } from 'vitest';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import Separator from './separator.svelte';
import type { SeparatorProps } from './types';

const hosts: HTMLElement[] = [];

/** A box of a known size, so a full-width or full-height line can be measured. */
const drawIn = async (
  size: { width: string; height: string },
  props: SeparatorProps = {},
): Promise<HTMLElement> => {
  const host = document.createElement('div');
  host.style.inlineSize = size.width;
  host.style.blockSize = size.height;
  document.body.append(host);
  hosts.push(host);

  await render(Separator, { target: host, props });

  const line = host.querySelector<HTMLElement>('[data-slot="separator"]');
  if (line === null) throw new Error('no separator rendered');
  return line;
};

afterEach(() => {
  for (const host of hosts.splice(0)) host.remove();
});

describe('Separator', () => {
  it('is a drawing a screen reader walks past', async () => {
    const line = await drawIn({ width: '200px', height: '40px' });

    expect(line.getAttribute('role')).toBe('none');
    expect(line.getAttribute('data-orientation')).toBe('horizontal');
    expect(line.getAttribute('aria-orientation')).toBeNull();
  });

  it('is announced when it marks a real break', async () => {
    const line = await drawIn(
      { width: '200px', height: '40px' },
      { decorative: false },
    );

    expect(line.getAttribute('role')).toBe('separator');
    // Horizontal is what the role already means, so it is not repeated.
    expect(line.getAttribute('aria-orientation')).toBeNull();
  });

  it('says which way it runs only when it stands on end', async () => {
    const line = await drawIn(
      { width: '200px', height: '40px' },
      { decorative: false, orientation: 'vertical' },
    );

    expect(line.getAttribute('role')).toBe('separator');
    expect(line.getAttribute('aria-orientation')).toBe('vertical');
  });

  it('draws one pixel across the box it sits in', async () => {
    const line = await drawIn({ width: '200px', height: '40px' });
    const box = line.getBoundingClientRect();

    expect(box.height).toBeCloseTo(1, 1);
    expect(box.width).toBeCloseTo(200, 1);
  });

  it('draws one pixel down the box when it stands on end', async () => {
    const line = await drawIn(
      { width: '200px', height: '40px' },
      { orientation: 'vertical' },
    );
    const box = line.getBoundingClientRect();

    expect(box.width).toBeCloseTo(1, 1);
    expect(box.height).toBeCloseTo(40, 1);
  });

  it('has nothing axe objects to', async () => {
    const line = await drawIn(
      { width: '200px', height: '40px' },
      { decorative: false },
    );

    await expectNoViolations(line);
  });
});
