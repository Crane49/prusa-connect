import { describe, expect, it } from 'vitest';

import { DeviceModels } from './device-models.svelte';
import type { DeviceModelsClient } from './services/device-models';

const entry = (model: string, toolChanger?: boolean) => ({
  connector: 'xbuddy',
  display_name: `Prusa ${model}`,
  icon: 'printer',
  kind: 'printer',
  model,
  vendor: 'Prusa',
  ...(toolChanger === undefined ? {} : { tool_changer: toolChanger }),
});

const stub = (fail = false) => {
  let calls = 0;
  const client: DeviceModelsClient = {
    getDeviceModelVocabulary: () => {
      calls += 1;

      return fail
        ? Promise.reject(new Error('down'))
        : Promise.resolve({
            connectors: [],
            refused: [],
            format: 'device-models',
            items: [
              entry('XL', true),
              { ...entry('MK4S'), device_type_id: '1.4.0' },
              entry('MMU3', false),
              { ...entry('Klipper'), display_name: 'Klipper printer' },
              {
                ...entry('Bench_One'),
                artwork: '/api/v1/vocabularies/device-models/Bench_One/artwork',
              },
            ],
            version: '',
            vocabulary: 'device-models',
          });
    },
  };

  return { client, calls: () => calls };
};

describe('DeviceModels', () => {
  // A registration names its model by the code the firmware reports, which
  // the vocabulary maps to the row a page draws from.
  it('finds a model by its own spelling and by the code firmware reports', async () => {
    const models = new DeviceModels(stub().client);
    await models.read();

    expect(models.entryOf('MK4S')?.model).toBe('MK4S');
    expect(models.entryOf('1.4.0')?.model).toBe('MK4S');
    expect(models.entryOf('9.9.9')).toBeUndefined();
    expect(models.entryOf('')).toBeUndefined();
    expect(models.entryOf(undefined)).toBeUndefined();
  });

  it('names a model by the spelling the device reports', async () => {
    const models = new DeviceModels(stub().client);
    await models.read();

    expect(models.displayName('XL')).toBe('Prusa XL');
    expect(models.displayName('Klipper')).toBe('Klipper printer');
    expect(models.displayName('Unknown')).toBeUndefined();
    expect(models.displayName('--')).toBeUndefined();
    expect(models.displayName(undefined)).toBeUndefined();
  });

  it('matches a spelling the way the server does', async () => {
    const models = new DeviceModels(stub().client);
    await models.read();

    expect(models.displayName('klipper')).toBe('Klipper printer');
    expect(models.displayName('KLIPPER ')).toBe('Klipper printer');
    expect(models.displayName('mk-4s')).toBe('Prusa MK4S');
  });

  // The cases of `connectorapi.FoldModel`'s own test, and one where the order
  // of the two steps shows: U+212A KELVIN SIGN lowercases to an ASCII k, and
  // the server drops it before it could.
  it('folds a spelling the way the server does', async () => {
    const models = new DeviceModels({
      getDeviceModelVocabulary: () =>
        Promise.resolve({
          connectors: [],
          refused: [],
          format: '',
          items: [entry('CoreOne_INDX'), entry('MK4'), entry('K1')],
          version: '',
          vocabulary: 'device-models',
        }),
    });
    await models.read();

    expect(models.displayName('core one indx')).toBe('Prusa CoreOne_INDX');
    expect(models.displayName('mk-4')).toBe('Prusa MK4');
    expect(models.displayName('--')).toBeUndefined();
    expect(models.displayName('\u212a1')).toBeUndefined();
  });

  it('hands a printer icon the declared name', async () => {
    const models = new DeviceModels(stub().client);
    await models.read();

    expect(models.declared('klipper')).toEqual({ name: 'Klipper printer' });
    expect(models.declared('bench one')).toEqual({
      name: 'Prusa Bench_One',
      src: '/api/v1/vocabularies/device-models/Bench_One/artwork',
    });
    expect(models.declared('Unknown')).toBeUndefined();
  });

  // Every accessor names the same row for a spelling: the folded name first,
  // then the device type code as written, as the server's profile lookup
  // finds one. A notice, the add-printer guide and the icon beside them read
  // different accessors, and two rules would name one printer twice.
  it('finds one row for a spelling in every accessor', async () => {
    const models = new DeviceModels(stub().client);
    await models.read();

    for (const spelling of ['Klipper', 'klipper', 'KLIPPER ', 'kl-ipper']) {
      expect(models.entryOf(spelling)?.model).toBe('Klipper');
      expect(models.displayName(spelling)).toBe('Klipper printer');
      expect(models.declared(spelling)).toEqual({ name: 'Klipper printer' });
    }

    expect(models.entryOf('1.4.0')?.model).toBe('MK4S');
    expect(models.displayName('1.4.0')).toBe('Prusa MK4S');
    expect(models.declared('1.4.0')).toEqual({ name: 'Prusa MK4S' });
    expect(models.entryOf('x-l')?.model).toBe('XL');
    // A code keeps its dots, because "1.3.10" and "13.1.0" fold to one.
    expect(models.entryOf('140')).toBeUndefined();
  });

  it('says where a declared drawing is served, and nothing for a model with none', async () => {
    const models = new DeviceModels(stub().client);
    await models.read();

    expect(models.artwork('Bench_One')).toBe(
      '/api/v1/vocabularies/device-models/Bench_One/artwork',
    );
    expect(models.artwork('bench-one')).toBe(
      '/api/v1/vocabularies/device-models/Bench_One/artwork',
    );
    expect(models.artwork('XL')).toBeUndefined();
    expect(models.artwork(undefined)).toBeUndefined();
  });

  it('reads once however often it is asked', async () => {
    const { client, calls } = stub();
    const models = new DeviceModels(client);

    await Promise.all([models.read(), models.read()]);
    await models.read();

    expect(calls()).toBe(1);
  });

  it('keeps the fault and names no model when the read fails', async () => {
    const { client, calls } = stub(true);
    const models = new DeviceModels(client);
    await models.read();

    expect(models.error?.message).toBe('down');
    expect(models.entryOf('XL')).toBeUndefined();

    await models.read();
    expect(calls()).toBe(2);
  });
});
