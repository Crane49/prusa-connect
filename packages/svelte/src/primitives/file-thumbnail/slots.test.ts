import { describe, expect, it } from 'vitest';

import { slots } from './slots';

const setup = (): {
  take: (name: string) => () => void;
  started: string[];
} => {
  const take = slots(2);
  const started: string[] = [];
  return {
    take: (name) =>
      take(() => {
        started.push(name);
      }),
    started,
  };
};

describe('slots', () => {
  it('starts two at once and the rest in the order they asked', () => {
    const { take, started } = setup();
    const a = take('a');
    take('b');
    take('c');
    take('d');

    expect(started).toEqual(['a', 'b']);

    a();
    expect(started).toEqual(['a', 'b', 'c']);
  });

  it('frees a slot once, however often it is given back', () => {
    const { take, started } = setup();
    const a = take('a');
    take('b');
    take('c');
    take('d');

    a();
    a();
    expect(started).toEqual(['a', 'b', 'c']);
  });

  // A row unmounted, or scrolled away, before its turn came.
  it('drops a job that leaves the queue before it started', () => {
    const { take, started } = setup();
    const a = take('a');
    take('b');
    const c = take('c');
    take('d');

    c();
    a();
    expect(started).toEqual(['a', 'b', 'd']);

    c();
    take('e');
    expect(started).toEqual(['a', 'b', 'd']);
  });

  it('never has more than two running through any churn', () => {
    const take = slots(2);
    let running = 0;
    let peak = 0;
    const jobs = Array.from({ length: 20 }, () => {
      let started = false;
      const release = take(() => {
        started = true;
        running += 1;
        peak = Math.max(peak, running);
      });
      return (): void => {
        if (started) running -= 1;
        release();
      };
    });

    // A scrambled order, so some finish and some leave before their turn.
    for (let step = 0; step < jobs.length; step += 1) {
      jobs[(step * 7) % jobs.length]?.();
    }

    expect(peak).toBe(2);
    expect(running).toBe(0);
  });
});
