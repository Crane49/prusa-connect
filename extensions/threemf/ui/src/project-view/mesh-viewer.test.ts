import { describe, expect, it } from 'vitest';

import { liftDark, rgbOf } from './mesh-viewer';

const brightness = ([r, g, b]: readonly [number, number, number]) =>
  0.2126 * r + 0.7152 * g + 0.0722 * b;

describe('liftDark', () => {
  it('lifts black to a visible grey', () => {
    const [r, g, b] = liftDark([0, 0, 0]);
    expect(r).toBeGreaterThan(0.3);
    expect(r).toBeCloseTo(g);
    expect(g).toBeCloseTo(b);
  });

  it('leaves a bright colour as it is', () => {
    expect(liftDark([0.9, 0.36, 0.08])).toEqual([0.9, 0.36, 0.08]);
  });

  it('lifts a dark colour and keeps its hue', () => {
    const lifted = liftDark([0.2, 0.02, 0.02]);
    expect(brightness(lifted)).toBeGreaterThan(brightness([0.2, 0.02, 0.02]));
    expect(lifted[0]).toBeGreaterThan(lifted[1]);
  });
});

describe('rgbOf', () => {
  it('reads a hex colour', () => {
    expect(rgbOf('#ff0000')).toEqual([1, 0, 0]);
  });

  it('answers undefined for an empty colour', () => {
    expect(rgbOf('')).toBeUndefined();
  });

  it('keeps a real black', () => {
    expect(rgbOf('#000')).toEqual([0, 0, 0]);
  });
});
