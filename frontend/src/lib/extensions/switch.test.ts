import { describe, expect, it } from 'vitest';

import type { ResourceInstance } from '@prusa/connect-ui/data';

import type { ExtensionRow } from './rows';
import { observedOf, removableOf, switchOf, whyOf } from './switch';

const row = (over: Partial<ExtensionRow> = {}): ExtensionRow =>
  ({
    key: 'k',
    module: 'github.com/acme/spools',
    title: 'Spools',
    summary: '',
    author: '',
    source: 'local',
    state: 'available',
    version: '1.0.0',
    platforms: [],
    permissions: [],
    pages: [],
    offer: { module: 'github.com/acme/spools' },
    ...over,
  }) as ExtensionRow;

const instance = (over: Record<string, unknown> = {}): ResourceInstance =>
  ({
    kind: 'asset',
    name: 'spools',
    enabled: true,
    source: 'overlay',
    module: { asked: {} },
    status: { state: 'ready' },
    ...over,
  }) as unknown as ResourceInstance;

describe('the switch of a row', () => {
  it('locks an installed module off and offers no install while modules are off', () => {
    expect(
      switchOf(row({ state: 'installed', instance: instance() }), true, false),
    ).toEqual({ kind: 'locked', on: false, reason: 'modules' });
    expect(switchOf(row(), true, false)).toEqual({ kind: 'none' });
    // A module the operator declared keeps the switch config.yaml gives it.
    expect(
      switchOf(
        row({ state: 'installed', instance: instance({ source: 'file' }) }),
        true,
        false,
      ),
    ).toEqual({ kind: 'locked', on: true, reason: 'file' });
  });

  it('names safe mode as the reason when modules are off for it', () => {
    expect(
      switchOf(
        row({ state: 'installed', instance: instance() }),
        true,
        false,
        true,
      ),
    ).toEqual({ kind: 'locked', on: false, reason: 'safeMode' });
  });

  it('installs a local offer', () => {
    expect(switchOf(row(), true)).toEqual({ kind: 'install' });
  });

  it('leaves a marketplace offer its Install button', () => {
    expect(switchOf(row({ source: 'marketplace' }), true)).toEqual({
      kind: 'none',
    });
  });

  it('flips an approved module', () => {
    expect(
      switchOf(
        row({ state: 'installed', instance: instance({ enabled: false }) }),
        true,
      ),
    ).toEqual({ kind: 'toggle', on: false });
  });

  it('locks a module a switched on module requires, and not one only switched off modules require', () => {
    const requiredBy = (enabled: boolean) =>
      row({
        state: 'installed',
        instance: instance({
          module: {
            asked: {},
            required_by: [
              {
                instance: 'sink:planner',
                module: 'com.github.aux.acme.planner',
                version: '^1.0',
                enabled,
              },
            ],
          },
        }),
      });

    expect(switchOf(requiredBy(true), true)).toEqual({
      kind: 'locked',
      on: true,
      reason: 'required',
      names: ['planner'],
    });
    // The server refuses the switch only while the dependant is on.
    expect(switchOf(requiredBy(false), true)).toEqual({
      kind: 'toggle',
      on: true,
    });
  });

  it('an unapproved module opens the install question for its version', () => {
    expect(
      switchOf(
        row({
          state: 'installed',
          installedVersion: '0.9.0',
          instance: instance({ enabled: false, module: {} }),
        }),
        true,
      ),
    ).toEqual({ kind: 'approve', version: '0.9.0' });
  });

  it('locks an unapproved module nothing offers', () => {
    expect(
      switchOf(
        row({
          source: 'unlisted',
          offer: undefined,
          state: 'installed',
          instance: instance({ enabled: false, module: {} }),
        }),
        true,
      ),
    ).toEqual({ kind: 'locked', on: false, reason: 'unapproved' });
  });

  it('locks a module the operator declared by hand', () => {
    expect(
      switchOf(
        row({ state: 'installed', instance: instance({ source: 'file' }) }),
        true,
      ),
    ).toEqual({ kind: 'locked', on: true, reason: 'file' });
  });

  it('shows a declared module off in safe mode', () => {
    expect(
      switchOf(
        row({ state: 'installed', instance: instance({ source: 'file' }) }),
        true,
        false,
        true,
      ),
    ).toEqual({ kind: 'locked', on: false, reason: 'safeMode' });
  });

  it('locks it for a person who may not configure it', () => {
    expect(
      switchOf(row({ state: 'installed', instance: instance() }), false),
    ).toEqual({ kind: 'locked', on: true, reason: 'access' });
    expect(switchOf(row(), false)).toEqual({
      kind: 'locked',
      on: false,
      reason: 'access',
    });
  });
});

const asked = (key: string, params?: Record<string, unknown>) =>
  params === undefined ? key : `${key} ${JSON.stringify(params)}`;

const memory = (over: Record<string, unknown> = {}) => ({
  switch_off_failures: 3,
  switch_off_window_seconds: 3600,
  ...over,
});

describe('what a module is doing', () => {
  it.each([
    ['ready', {}, 'running'],
    ['degraded', {}, 'degraded'],
    ['pending', {}, 'starting'],
    ['starting', {}, 'starting'],
    ['retrying', {}, 'retrying'],
    ['failed', {}, 'failed'],
    ['disabled', {}, 'stopped'],
    ['stopping', {}, 'stopped'],
    ['stopped', {}, 'stopped'],
    ['orphaned', {}, 'stopped'],
    [
      'retrying',
      { memory: memory({ switched_off: { at: 'now', stops: [] } }) },
      'switched-off',
    ],
    [
      'ready',
      { memory: memory({ switched_off: { at: 'now', stops: [] } }) },
      'switched-off',
    ],
  ])('%s %j reads as %s', (state, over, observed) => {
    expect(observedOf(instance({ status: { state, ...over } }))).toBe(observed);
  });
});

describe('whether a row can be removed', () => {
  const installed = (source: string) =>
    row({
      state: 'installed',
      namespace: 'com.github.aux.acme.spools',
      instance: instance({ source }),
    });

  it('removes what the server installed, and leaves unknown access to the server', () => {
    expect(removableOf(installed('overlay'), true)).toBe(true);
    expect(removableOf(installed('overlay'), undefined)).toBe(true);
  });

  it('removes nothing config.yaml declares, nothing not installed, and nothing for a person refused', () => {
    expect(removableOf(installed('file'), true)).toBe(false);
    expect(removableOf(row(), true)).toBe(false);
    expect(removableOf(installed('overlay'), false)).toBe(false);
  });
});

describe('why a module is not running', () => {
  const waiting = (conditions: unknown[], state = 'retrying') =>
    instance({ status: { state, conditions } });

  it('names the record backend a module waits for', () => {
    const why = whyOf(
      waiting([
        {
          type: 'Started',
          status: 'False',
          reason: 'StartFailed',
          message: 'core.internal/RESOURCE_RECORDS_UNSERVED: started badly',
        },
        {
          type: 'DependenciesSatisfied',
          status: 'False',
          reason: 'DependencyMissing',
          message:
            'core.internal/RESOURCE_RECORDS_UNSERVED: the module keeps its data in the record store, and no record backend is serving',
        },
      ]),
      asked,
    );

    expect(why?.code).toBe('core.internal/RESOURCE_RECORDS_UNSERVED');
    expect(why?.detail ?? why?.title).toContain('no record backend is serving');
  });

  it('says nothing for a running or a stopped module', () => {
    const failing = [
      { type: 'Started', status: 'False', reason: 'X', message: 'broken' },
    ];

    expect(whyOf(waiting(failing, 'ready'), asked)).toBeUndefined();
    expect(whyOf(waiting(failing, 'disabled'), asked)).toBeUndefined();
  });

  it('says nothing when no condition holds a message', () => {
    expect(
      whyOf(
        waiting([{ type: 'Started', status: 'False', reason: 'X' }]),
        asked,
      ),
    ).toBeUndefined();
  });

  it('names the breaker for a degraded module', () => {
    const why = whyOf(
      waiting(
        [
          {
            type: 'BreakerClosed',
            status: 'False',
            reason: 'Open',
            message: 'core.extension/BREAKER_OPEN: calls keep failing',
          },
        ],
        'degraded',
      ),
      asked,
    );

    expect(why?.code).toBe('core.extension/BREAKER_OPEN');
  });

  it('gives the last stop of a retrying module before any condition', () => {
    const why = whyOf(
      instance({
        status: {
          state: 'retrying',
          conditions: [
            { type: 'Started', status: 'False', reason: 'X', message: 'late' },
          ],
          memory: memory({
            last_stop: { at: 'now', result: 'not-responding' },
          }),
        },
      }),
      asked,
    );

    expect(why?.title).toBe('app.settings.extensions.stop.notResponding');
  });

  it('says a switched-off module was switched off, with the failures in force', () => {
    const why = whyOf(
      instance({
        status: {
          state: 'retrying',
          memory: memory({
            last_stop: { at: 'now', result: 'exited', code: 1 },
            switched_off: {
              at: 'now',
              stops: [{}, {}, {}].map(() => ({ at: 'now', result: 'exited' })),
            },
          }),
        },
      }),
      asked,
    );

    expect(why?.title).toContain('stop.switchedOff');
    expect(why?.title).toContain('"count":3');
  });

  it('says what stopped a stopped module, and nothing without a record', () => {
    const stopped = (over: Record<string, unknown>) =>
      instance({ status: { state: 'stopped', ...over } });

    expect(
      whyOf(
        stopped({
          memory: memory({
            last_stop: { at: 'now', result: 'stopped-by-operator' },
          }),
        }),
        asked,
      )?.title,
    ).toBe('app.settings.extensions.stop.operator');
    expect(whyOf(stopped({}), asked)).toBeUndefined();
  });

  it('puts a missing dependency before an older stop', () => {
    const why = whyOf(
      instance({
        status: {
          state: 'failed',
          conditions: [
            {
              type: 'DependenciesSatisfied',
              status: 'False',
              reason: 'DependencyMissing',
              message:
                'core.internal/RESOURCE_RECORDS_UNSERVED: no record backend is serving',
            },
          ],
          memory: memory({ last_stop: { at: 'now', result: 'exited' } }),
        },
      }),
      asked,
    );

    expect(why?.code).toBe('core.internal/RESOURCE_RECORDS_UNSERVED');
  });
});
