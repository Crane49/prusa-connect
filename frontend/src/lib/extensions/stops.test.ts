import { describe, expect, it } from 'vitest';

import {
  overBudgetWarning,
  type ResourceMemory,
  stopText,
  windowText,
} from './stops';

const asked = (key: string, params?: Record<string, unknown>) =>
  params === undefined ? key : `${key} ${JSON.stringify(params)}`;

const MIB = 1024 * 1024;

const memory = (over: Partial<ResourceMemory> = {}): ResourceMemory =>
  ({
    switch_off_failures: 3,
    switch_off_window_seconds: 3600,
    on_over_memory: 'stop',
    over_budget: false,
    ...over,
  }) as ResourceMemory;

const stop = (result: string, over: Record<string, unknown> = {}) => ({
  at: '2026-10-30T10:00:00Z',
  result,
  ...over,
});

describe('stopText', () => {
  it('names what the process used against its budget', () => {
    const text = stopText(
      stop('over-memory', { used_bytes: 140 * MIB, budget_bytes: 128 * MIB }),
      asked,
    );

    expect(text).toContain('stop.overMemory ');
    expect(text).toContain('"used"');
    expect(text).toContain('"budget"');
  });

  it('says a hung module was restarted', () => {
    expect(stopText(stop('not-responding'), asked)).toBe(
      'app.settings.extensions.stop.notResponding',
    );
  });

  it('reads the exit code and the signal when there is one', () => {
    expect(stopText(stop('exited', { code: 2 }), asked)).toContain('"code":2');
    expect(stopText(stop('exited'), asked)).toContain('exitedPlain');
    expect(stopText(stop('signal', { signal: 'SIGSEGV' }), asked)).toContain(
      '"signal":"SIGSEGV"',
    );
    expect(stopText(stop('signal'), asked)).toContain('signalPlain');
  });

  it('says an operator stopped it', () => {
    expect(stopText(stop('stopped-by-operator'), asked)).toContain('operator');
  });

  it('counts the failures of the record within the configured window', () => {
    const text = stopText(
      stop('switched-off-by-host'),
      asked,
      memory({
        switch_off_failures: 5,
        switch_off_window_seconds: 900,
        switched_off: {
          at: '2026-10-30T10:00:00Z',
          stops: [stop('exited'), stop('exited'), stop('exited')],
        },
      }),
    );

    expect(text).toContain('"count":3');
    expect(text).not.toContain('Plain');
  });

  it('names no count when the record or the window is missing', () => {
    expect(stopText(stop('switched-off-by-host'), asked)).toContain(
      'switchedOffPlain',
    );
  });

  it('puts a result it has no wording for in a neutral sentence', () => {
    expect(stopText(stop('give-up'), asked)).toBe(
      'app.settings.extensions.stop.other {"result":"give-up"}',
    );
  });
});

describe('overBudgetWarning', () => {
  it('warns only for a module set to warn that is over budget', () => {
    expect(
      overBudgetWarning(
        memory({ on_over_memory: 'warn', over_budget: true }),
        asked,
      ),
    ).toBeDefined();
    expect(
      overBudgetWarning(memory({ on_over_memory: 'warn' }), asked),
    ).toBeUndefined();
    expect(
      overBudgetWarning(memory({ over_budget: true }), asked),
    ).toBeUndefined();
    expect(overBudgetWarning(undefined, asked)).toBeUndefined();
  });
});

describe('windowText', () => {
  it('writes a whole hour without a minute part', () => {
    expect(windowText(3600)).toBe('1 hour');
    expect(windowText(7200)).toBe('2 hours');
  });

  it('keeps the minutes of a part hour', () => {
    expect(windowText(5400)).toBe('1 hr, 30 min');
    expect(windowText(900)).toBe('15 min');
  });
});
