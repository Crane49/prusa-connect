import { afterEach, describe, expect, it } from 'vitest';
import { render } from 'vitest-browser-svelte';

import { expectNoViolations } from '../../test/axe';
import Logo from './logo.svelte';
import type { LogoProps } from './types';

const hosts: HTMLElement[] = [];

/**
 * The mark inside a box wider than it needs, which is where `w-fit` shows.
 * A mark in a box its own size proves nothing about stretching.
 */
const drawIn = async (
  width: string,
  props: LogoProps = {},
): Promise<HTMLElement> => {
  const host = document.createElement('div');
  host.style.inlineSize = width;
  host.style.display = 'flex';
  host.style.flexDirection = 'column';
  document.body.append(host);
  hosts.push(host);

  await render(Logo, { target: host, props });

  const mark = host.querySelector<HTMLElement>('[data-slot="logo"]');
  if (mark === null) throw new Error('no mark rendered');
  return mark;
};

const parts = (mark: HTMLElement): HTMLElement[] =>
  [...mark.children] as HTMLElement[];

const fontSize = (element: HTMLElement): number =>
  Number.parseFloat(getComputedStyle(element).fontSize);

afterEach(() => {
  for (const host of hosts.splice(0)) host.remove();
});

describe('Logo', () => {
  it('is text a person can select, not a picture of text', async () => {
    const mark = await drawIn('400px');

    expect(mark.querySelector('img')).toBeNull();
    expect(mark.querySelector('svg')).toBeNull();
    expect(parts(mark).map((part) => part.textContent)).toEqual([
      'PRUSA',
      'Connect',
    ]);
  });

  it('declares no colour of its own, so the ground decides', async () => {
    const host = document.createElement('div');
    host.style.color = 'rgb(0, 0, 0)';
    document.body.append(host);
    hosts.push(host);

    await render(Logo, { target: host });
    const mark = host.querySelector<HTMLElement>('[data-slot="logo"]');

    for (const element of [mark!, ...parts(mark!)]) {
      expect(getComputedStyle(element).color).toBe('rgb(0, 0, 0)');
    }
  });

  it('sets the name at 0.49em of the wordmark at every size', async () => {
    // The width match is a proportion, so it is held by the ratio of the two
    // type sizes. Nothing is letter-spaced and nothing is measured at run
    // time: change the ratio and the second line stops spanning the mark.
    //
    // The drawn widths are not compared here. 0.49em matches the two lines in
    // Atlas Grotesk, the library ships no `@font-face` for it, and the test
    // browser therefore sets the mark in the fallback stack, where the two
    // lines are not the same width. The `Name Matches The Mark Width` story
    // is where a person sees the match in the real face.
    for (const size of ['sm', 'md', 'lg'] as const) {
      const mark = await drawIn('400px', { size });
      const [wordmark, name] = parts(mark);

      expect(fontSize(mark)).toBe(fontSize(wordmark!));
      expect(fontSize(name!) / fontSize(mark)).toBeCloseTo(0.49, 2);
      expect(getComputedStyle(name!).letterSpacing).toBe('normal');
    }
  });

  it('grows with its size and keeps one ratio through all three', async () => {
    const drawn = [];
    for (const size of ['sm', 'md', 'lg'] as const) {
      drawn.push(fontSize(await drawIn('400px', { size })));
    }
    const [small, medium, large] = drawn as [number, number, number];

    expect(small).toBeLessThan(medium);
    expect(medium).toBeLessThan(large);
    // `lg` reaches past the interface's type scale, at 1.6 of `--text-2xl`.
    expect(large / medium).toBeCloseTo(1.6, 2);
  });

  it('takes the width of the mark and not of the column it sits in', async () => {
    const mark = await drawIn('400px');

    expect(mark.getBoundingClientRect().width).toBeGreaterThan(0);
    expect(mark.getBoundingClientRect().width).toBeLessThan(400);
  });

  it('closes the gap the line boxes leave between the two lines', async () => {
    const mark = await drawIn('400px');
    const [wordmark, name] = parts(mark);
    const pull = Number.parseFloat(getComputedStyle(name!).marginTop);

    // `em` in a margin resolves against the element's own font size, so the
    // pull is 0.22 of the second line and not of the mark. The second line is
    // a fixed share of the mark, so the pull still holds at every size.
    expect(pull).toBeCloseTo(-0.22 * fontSize(name!), 1);
    expect(name!.getBoundingClientRect().top).toBeLessThan(
      wordmark!.getBoundingClientRect().bottom,
    );
  });

  it('gives each word its own line in the narrow shape', async () => {
    const mark = await drawIn('400px', { lines: 'three' });

    expect(parts(mark).map((part) => part.textContent)).toEqual([
      'PRUSA',
      'Connect',
    ]);
  });

  it('draws the bare wordmark when there is no name to set', async () => {
    const mark = await drawIn('400px', { subtitle: '' });

    expect(parts(mark).map((part) => part.textContent)).toEqual(['PRUSA']);
  });

  it('sets a name of its caller’s own', async () => {
    const mark = await drawIn('400px', { subtitle: 'Connect' });

    expect(parts(mark).map((part) => part.textContent)).toEqual([
      'PRUSA',
      'Connect',
    ]);
  });

  it('has nothing axe objects to', async () => {
    const mark = await drawIn('400px', { size: 'lg' });

    await expectNoViolations(mark);
  });
});
