// The address spells the page's state, and reading it back gives the same
// state, so a reload or a link lands on the same view.
import { describe, expect, it } from 'vitest';

import { DEFAULT_STATE, type PageState, searchOf, stateOf } from './page-state';

describe('the page state in the address', () => {
  it('leaves every default out, so the bare page has a bare address', () => {
    expect(searchOf(DEFAULT_STATE)).toBe('');
    expect(stateOf('')).toEqual(DEFAULT_STATE);
  });

  it('reads back what it wrote', () => {
    const state: PageState = {
      tab: 'metrics',
      logs: {
        origin: { kind: 'device', id: 'xl' },
        level: 'warning',
        component: 'Marlin',
        text: 'heatbreak',
        window: 6,
      },
      metrics: {
        origin: { kind: 'mac', mac: '10:9c:70:26:7a:00' },
        window: 24,
        name: 'temp_noz',
        field: 'value',
      },
      status: true,
      guide: true,
    };

    expect(stateOf(searchOf(state))).toEqual(state);
  });

  it('takes the default for a value it does not know', () => {
    const read = stateOf('?tab=nope&level=loud&hours=5&mhours=x');

    expect(read.tab).toBe('logs');
    expect(read.logs.level).toBeUndefined();
    expect(read.logs.window).toBe(DEFAULT_STATE.logs.window);
    expect(read.metrics.window).toBe(DEFAULT_STATE.metrics.window);
  });
});
