import { describe, expect, it } from 'vitest';

import {
  configOf,
  controlOf,
  draftOf,
  SECRET_KEPT,
  type ResourceOption,
} from './module-settings';

const option = (
  key: string,
  type: ResourceOption['type'],
  over: Partial<ResourceOption> = {},
): ResourceOption => ({ key, type, summary: '', ...over });

const OPTIONS = [
  option('shelf_size', 'int', { required: true }),
  option('ratio', 'number'),
  option('enabled', 'bool'),
  option('endpoint', 'url'),
  option('token', 'secret'),
  option('tags', 'list'),
  option('limits', 'object'),
  option('every', 'duration'),
];

describe('the field of an option', () => {
  it.each([
    ['string', 'text'],
    ['url', 'text'],
    ['duration', 'text'],
    ['int', 'number'],
    ['number', 'number'],
    ['bool', 'switch'],
    ['secret', 'secret'],
    ['list', 'json'],
    ['object', 'json'],
  ] as const)('%s is a %s field', (type, control) => {
    expect(controlOf(option('k', type))).toBe(control);
  });
});

describe('a draft of the settings', () => {
  it('shows written values and hides a stored secret', () => {
    const { draft, stored } = draftOf(OPTIONS, {
      shelf_size: 4,
      token: SECRET_KEPT,
      tags: ['a'],
      enabled: true,
    });

    expect(draft.shelf_size).toBe('4');
    expect(draft.token).toBe('');
    expect(draft.tags).toBe('["a"]');
    expect(draft.enabled).toBe(true);
    expect(stored).toEqual(new Set(['token']));
  });
});

describe('the config a draft saves', () => {
  it('parses each value as its type and leaves empty fields to their default', () => {
    const { config, errors } = configOf(
      OPTIONS,
      {
        shelf_size: '6',
        ratio: '0.5',
        enabled: false,
        endpoint: '',
        token: '',
        tags: '["a","b"]',
        limits: '',
        every: '5m',
      },
      new Set(),
    );

    expect(errors).toEqual({});
    expect(config).toEqual({
      shelf_size: 6,
      ratio: 0.5,
      enabled: false,
      tags: ['a', 'b'],
      every: '5m',
    });
  });

  it('keeps a stored secret the person did not touch', () => {
    expect(
      configOf(OPTIONS, { shelf_size: '1', token: '' }, new Set(['token']))
        .config.token,
    ).toBe(SECRET_KEPT);
    expect(
      configOf(OPTIONS, { shelf_size: '1', token: 'new' }, new Set(['token']))
        .config.token,
    ).toBe('new');
    // Cleared: not kept, nothing typed, so the key is left out.
    expect(
      configOf(OPTIONS, { shelf_size: '1', token: '' }, new Set()).config,
    ).not.toHaveProperty('token');
  });

  it('draws an unset switch at its default and leaves it out while it stays there', () => {
    const options = [option('verbose', 'bool', { default: 'true' })];

    expect(draftOf(options, {}).draft.verbose).toBe(true);
    expect(draftOf(options, { verbose: false }).draft.verbose).toBe(false);
    expect(configOf(options, { verbose: true }, new Set()).config).toEqual({});
    expect(configOf(options, { verbose: false }, new Set()).config).toEqual({
      verbose: false,
    });
    expect(
      configOf(options, { verbose: true }, new Set(), { verbose: false })
        .config,
    ).toEqual({ verbose: true });
  });

  it('keeps a key no option declares as it was', () => {
    expect(
      configOf(OPTIONS, { shelf_size: '2' }, new Set(), {
        shelf_size: 1,
        legacy: { a: 1 },
      }).config,
    ).toEqual({ shelf_size: 2, legacy: { a: 1 } });
  });

  it('names what is wrong with each field', () => {
    const { errors } = configOf(
      OPTIONS,
      {
        shelf_size: '1.5',
        ratio: 'x',
        endpoint: 'not a url',
        tags: '{}',
        limits: '[]',
      },
      new Set(),
    );
    expect(errors).toEqual({
      shelf_size: 'integer',
      ratio: 'number',
      endpoint: 'url',
      tags: 'list',
      limits: 'object',
    });
    expect(configOf(OPTIONS, { shelf_size: '' }, new Set()).errors).toEqual({
      shelf_size: 'required',
    });
  });
});
