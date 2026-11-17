import { afterEach, describe, expect, it } from 'vitest';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import AspectRatio from './aspect-ratio.svelte';
import type { AspectRatioProps } from './types';

const hosts: HTMLElement[] = [];

const nextFrame = () =>
  new Promise<void>((resolve) => {
    requestAnimationFrame(() => {
      resolve();
    });
  });

const drawIn = async (
  width: string,
  props: AspectRatioProps = {},
): Promise<{ host: HTMLElement; box: HTMLElement }> => {
  const host = document.createElement('div');
  host.style.inlineSize = width;
  document.body.append(host);
  hosts.push(host);

  await render(AspectRatio, { target: host, props });

  const box = host.querySelector<HTMLElement>('[data-slot="aspect-ratio"]');
  if (box === null) throw new Error('no box rendered');
  return { host, box };
};

afterEach(() => {
  for (const host of hosts.splice(0)) host.remove();
});

describe('AspectRatio', () => {
  it('keeps the shape it was given at two different widths', async () => {
    const { host, box } = await drawIn('320px', { ratio: 16 / 9 });

    const wide = box.getBoundingClientRect();
    expect(wide.width).toBeCloseTo(320, 1);
    expect(wide.height).toBeCloseTo(180, 1);

    host.style.inlineSize = '160px';
    await nextFrame();

    const narrow = box.getBoundingClientRect();
    expect(narrow.width).toBeCloseTo(160, 1);
    expect(narrow.height).toBeCloseTo(90, 1);
  });

  it('is square when no ratio is given', async () => {
    const { box } = await drawIn('240px');
    const square = box.getBoundingClientRect();

    expect(square.height).toBeCloseTo(square.width, 1);
  });

  it('has nothing axe objects to', async () => {
    const { host } = await drawIn('240px', { ratio: 4 / 3 });

    await expectNoViolations(host);
  });
});
