import { describe, expect, it } from 'vitest';

import {
  type AccessState,
  accessStateOf,
  callerLevelOf,
  planAccessChoice,
  planAccessOn,
} from './printer-access';

const ADMIN = 3;

const blocks = (state: AccessState): Record<string, string | undefined> =>
  state.kind === 'editable'
    ? Object.fromEntries(state.options.map((o) => [o.value, o.blocked]))
    : {};

describe('accessStateOf', () => {
  it('raises an organization viewer to operator on one printer', () => {
    const state = accessStateOf('viewer', ['operator'], false, ADMIN);

    expect(state).toMatchObject({
      kind: 'editable',
      on: true,
      choice: 'operator',
      inert: false,
    });
    expect(blocks(state)).toEqual({
      org: undefined,
      viewer: 'inOrg',
      operator: undefined,
      manager: undefined,
    });
  });

  it('shows a printer grant the organization role outranks, marked inert', () => {
    const state = accessStateOf('operator', ['viewer'], false, ADMIN);

    expect(state).toMatchObject({ on: true, choice: 'viewer', inert: true });
  });

  it('shows the highest of two printer grants written side by side', () => {
    expect(
      accessStateOf('', ['viewer', 'operator'], false, ADMIN),
    ).toMatchObject({ choice: 'operator', on: true });
  });

  it('draws a person with no role anywhere as off, with no organization to follow', () => {
    const state = accessStateOf('', [], false, ADMIN);

    expect(state).toMatchObject({ on: false, choice: 'org' });
    expect(blocks(state).org).toBe('noOrgRole');
  });

  it('keeps the printer role under a block, so on gives it back', () => {
    expect(
      accessStateOf('operator', ['blocked', 'manager'], false, ADMIN),
    ).toMatchObject({ on: false, choice: 'manager' });
  });

  it('locks an organization administrator, the caller and an owner', () => {
    expect(accessStateOf('admin', [], false, ADMIN)).toEqual({
      kind: 'locked',
      lock: 'orgAdmin',
    });
    expect(accessStateOf('viewer', [], true, ADMIN)).toEqual({
      kind: 'locked',
      lock: 'self',
    });
    expect(accessStateOf('viewer', ['owner'], false, ADMIN)).toEqual({
      kind: 'locked',
      lock: 'owner',
    });
  });

  it('refuses what the caller does not hold on this printer', () => {
    const state = accessStateOf('viewer', ['manager'], false, 2);

    expect(blocks(state).manager).toBe('aboveCaller');
    expect(state).toMatchObject({ switchBlocked: 'aboveCaller' });
  });
});

describe('callerLevelOf', () => {
  it('reads the highest action the caller holds', () => {
    expect(callerLevelOf(['observe', 'operate', 'administer'])).toBe(3);
    expect(callerLevelOf(['observe', 'operate'])).toBe(2);
    expect(callerLevelOf(['observe'])).toBe(1);
    expect(callerLevelOf(undefined)).toBe(0);
  });
});

describe('planAccessOn', () => {
  it('blocks and keeps the printer role', () => {
    expect(planAccessOn('viewer', ['operator'], false)).toEqual([
      { op: 'write', relation: 'blocked' },
    ]);
  });

  it('lifts a block', () => {
    expect(planAccessOn('viewer', ['blocked'], true)).toEqual([
      { op: 'withdraw', relation: 'blocked' },
    ]);
  });

  it('gives viewer to a person with no role anywhere, before lifting the block', () => {
    expect(planAccessOn('', ['blocked'], true)).toEqual([
      { op: 'write', relation: 'viewer' },
      { op: 'withdraw', relation: 'blocked' },
    ]);
    expect(planAccessOn('', [], true)).toEqual([
      { op: 'write', relation: 'viewer' },
    ]);
  });

  it('does nothing when the switch is already where it is asked to be', () => {
    expect(planAccessOn('viewer', ['blocked'], false)).toEqual([]);
  });
});

describe('planAccessChoice', () => {
  it('writes the new role before withdrawing the old one', () => {
    expect(planAccessChoice(['operator'], 'viewer')).toEqual([
      { op: 'write', relation: 'viewer' },
      { op: 'withdraw', relation: 'operator' },
    ]);
  });

  it('withdraws every printer role to follow the organization, and leaves a block', () => {
    expect(planAccessChoice(['viewer', 'operator', 'blocked'], 'org')).toEqual([
      { op: 'withdraw', relation: 'viewer' },
      { op: 'withdraw', relation: 'operator' },
    ]);
  });

  it('does nothing for the role already held', () => {
    expect(planAccessChoice(['operator'], 'operator')).toEqual([]);
  });
});
