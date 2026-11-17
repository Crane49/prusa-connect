import { createRawSnippet } from 'svelte';
import { describe, expect, it } from 'vitest';
import { render } from 'vitest-browser-svelte';

import type { HeaterState } from '../../domain';
import { expectNoViolations } from '../../test/axe';
import HeatReading from './heat-reading.svelte';
import TemperatureReading from './heating-display.svelte';
import { tints } from './variants';

const icon = createRawSnippet(() => ({
  render: () => '<svg aria-hidden="true" class="size-3.5"></svg>',
}));

const reading = createRawSnippet(() => ({ render: () => '<span>215</span>' }));

/** The rendered text with its layout whitespace taken out. */
const printed = (element: Element): string =>
  element.textContent.replace(/\s+/gu, '');

const display = (container: Element): HTMLElement => {
  const found = container.querySelector<HTMLElement>(
    '[data-slot="heating-display"]',
  );
  if (found === null) throw new Error('no reading rendered');
  return found;
};

/** Every state, and what the reading behind it is. */
const STATES: [HeaterState, number | undefined, number | undefined][] = [
  ['heating', 48, 215],
  ['at-temperature', 214, 215],
  ['cooling', 180, 60],
  ['off', 22, undefined],
  ['not-reported', undefined, undefined],
];

describe('HeatReading', () => {
  it('tints each state on its own and leaves the arrived one alone', async () => {
    for (const [state] of STATES) {
      const { container, unmount } = await render(HeatReading, {
        props: { state, children: reading },
      });
      const span = display(container);

      for (const name of tints[state].split(' ').filter(Boolean)) {
        expect(span.classList.contains(name)).toBe(true);
      }
      expect(span.getAttribute('data-variant')).toBe(state);
      // `at-temperature` names no colour, so it keeps whatever the caller set.
      expect(span.classList.contains('text-warning-text')).toBe(
        state === 'heating',
      );
      expect(span.classList.contains('text-info-text')).toBe(
        state === 'cooling',
      );
      await unmount();
    }
  });

  it('marks the direction with its own shape and hides it from a screen reader', async () => {
    const drawn: Record<string, string | undefined> = {};

    for (const [state] of STATES) {
      const { container, unmount } = await render(HeatReading, {
        props: { state, children: reading },
      });
      const span = display(container);
      const glyph = span.querySelector('svg');

      if (glyph !== null) {
        expect(glyph.getAttribute('aria-hidden')).toBe('true');
        // The word beside the reading carries the direction in language, so
        // the arrow is never part of the accessible name.
        expect(printed(span)).toBe('215');
      }
      drawn[state] = glyph?.innerHTML;
      await unmount();
    }

    expect(drawn['at-temperature']).toBeUndefined();
    expect(drawn.off).toBeUndefined();
    expect(drawn['not-reported']).toBeUndefined();
    // Two shapes, each meaning one thing, rather than one shape turned over.
    expect(drawn.heating).toBeDefined();
    expect(drawn.cooling).toBeDefined();
    expect(drawn.heating).not.toBe(drawn.cooling);
  });

  it('claims no trend for a reading nobody can refresh', async () => {
    const { container } = await render(HeatReading, {
      props: { state: 'heating', muted: true, children: reading },
    });
    const span = display(container);

    expect(span.querySelector('svg')).toBeNull();
    expect(span.classList.contains('animate-heating-pulse')).toBe(false);
    expect(span.classList.contains('text-warning-text')).toBe(false);
  });

  it('pulses only while a heater is still climbing', async () => {
    for (const [state] of STATES) {
      const { container, unmount } = await render(HeatReading, {
        props: { state, children: reading },
      });
      const running = getComputedStyle(display(container)).animationName;

      expect(running === 'none').toBe(state !== 'heating');
      await unmount();
    }
  });
});

describe('TemperatureReading', () => {
  it('prints whole degrees and the target it is working towards', async () => {
    const { container } = await render(TemperatureReading, {
      props: { icon, current: 214.6, target: 215 },
    });

    // 214.6 is at temperature and prints as 215. The rounding is the screen's,
    // and the state came from the reading the printer actually sent.
    expect(printed(display(container))).toBe('215°/215°');
    expect(display(container).getAttribute('data-variant')).toBe(
      'at-temperature',
    );
  });

  it('draws a dash where there is no reading at all', async () => {
    const { container } = await render(TemperatureReading, {
      props: { icon, current: undefined, target: 215 },
    });
    const span = display(container);

    expect(printed(span)).toBe('—');
    expect(span.querySelector('svg')).toBeNull();
    expect(span.classList.contains('text-muted-foreground')).toBe(true);
  });

  it('leaves out a target of zero, which is a heater switched off', async () => {
    for (const target of [0, undefined]) {
      const { container, unmount } = await render(TemperatureReading, {
        props: { icon, current: 48, target },
      });

      // `48°/0°` would read as a target rather than as the absence of one.
      expect(printed(display(container))).toBe('48°');
      expect(display(container).getAttribute('data-variant')).toBe('cooling');
      await unmount();
    }
  });

  it('keeps the target quiet while the reading beside it pulses', async () => {
    const { container } = await render(TemperatureReading, {
      props: { icon, current: 48, target: 215 },
    });
    const span = display(container);
    const target = span.querySelector('span');

    expect(span.classList.contains('animate-heating-pulse')).toBe(true);
    expect(target?.classList.contains('text-muted-foreground')).toBe(true);
    expect(getComputedStyle(target!).animationName).toBe('none');
  });

  it('has nothing axe objects to, with a reading and without one', async () => {
    const shown = await render(TemperatureReading, {
      props: { icon, current: 48, target: 215 },
    });
    await expectNoViolations(shown.container);
    await shown.unmount();

    const absent = await render(TemperatureReading, {
      props: { icon, current: undefined, target: undefined },
    });
    await expectNoViolations(absent.container);
  });
});
