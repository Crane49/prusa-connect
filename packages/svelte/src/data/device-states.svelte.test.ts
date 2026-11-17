// The moves each device kind declared, as this server publishes them. A screen
// asks this before it tells a person which states a printer takes a verb from,
// so a store that answered twice, or that let a failed read look like a kind
// with no moves at all, would put a wrong sentence on a screen.
import { describe, expect, it } from 'vitest';

import { DeviceStates } from './device-states.svelte';
import type {
  DeviceStateKind,
  DeviceStatesClient,
} from './services/device-states';
import { StubApiError } from './stubs';

const printer: DeviceStateKind = {
  connector: 'core/xbuddy',
  connectors: ['xbuddy'],
  guarded: true,
  kind: 'printer',
  namespace: 'core',
  states: [
    {
      accepts: ['core.command.start_print'],
      display_name: 'Idle',
      state: 'IDLE',
    },
    { accepts: [], display_name: 'Printing', state: 'PRINTING' },
  ],
  transitions: [
    { from: 'IDLE', to: 'PRINTING', verb: 'core.command.start_print' },
    { from: 'PRINTING', to: 'IDLE' },
  ],
};

const cutter: DeviceStateKind = {
  connector: 'example/cutter',
  connectors: ['cutter'],
  guarded: true,
  kind: 'cutter',
  namespace: 'example',
  states: [
    {
      accepts: ['core.command.start_print', 'example.command.cut'],
      display_name: 'Idle',
      state: 'IDLE',
    },
  ],
  transitions: [
    { from: 'IDLE', to: 'PRINTING', verb: 'core.command.start_print' },
    { from: 'IDLE', to: 'CUTTING', verb: 'example.command.cut' },
  ],
};

/** A server answering the vocabulary, counting how often it was asked. */
const stub = (
  items: readonly DeviceStateKind[] | null,
): { client: DeviceStatesClient; readonly calls: number } => {
  let calls = 0;

  return {
    client: {
      getDeviceStateVocabulary: () => {
        calls += 1;

        return Promise.resolve({
          format: 'device-states',
          items: items === null ? null : [...items],
          version: '',
          vocabulary: 'device-states',
        });
      },
    },
    get calls() {
      return calls;
    },
  };
};

describe('before the server has answered', () => {
  it('holds no kind and says it has not asked', () => {
    const states = new DeviceStates(stub([printer]).client);

    expect(states.asked).toBe(false);
    expect(states.kinds).toEqual([]);
    expect(states.transitionsOf(['printer'])).toEqual([]);
  });
});

describe('what the server declared', () => {
  it('hands back the moves of the kinds that were asked for', async () => {
    const states = new DeviceStates(stub([printer, cutter]).client);
    await states.read();

    expect(states.asked).toBe(true);
    expect(states.transitionsOf(['printer'])).toEqual(printer.transitions);
    expect(states.transitionsOf(['cutter'])).toEqual(cutter.transitions);
  });

  it('leaves out a kind nobody asked about', async () => {
    const states = new DeviceStates(stub([printer, cutter]).client);
    await states.read();

    expect(states.transitionsOf([])).toEqual([]);
    expect(states.transitionsOf(['toaster'])).toEqual([]);
  });

  // A fleet holds machines of more than one kind and the picker is handed one
  // table for all of them, so a move two kinds declare alike is one move.
  it('keeps a move two kinds declare alike only once', async () => {
    const states = new DeviceStates(stub([printer, cutter]).client);
    await states.read();

    expect(states.transitionsOf(['printer', 'cutter'])).toEqual([
      { from: 'IDLE', to: 'PRINTING', verb: 'core.command.start_print' },
      { from: 'PRINTING', to: 'IDLE' },
      { from: 'IDLE', to: 'CUTTING', verb: 'example.command.cut' },
    ]);
  });

  it('reads a kind that declared no move as no move', async () => {
    const states = new DeviceStates(
      stub([{ ...printer, transitions: null }]).client,
    );
    await states.read();

    expect(states.transitionsOf(['printer'])).toEqual([]);
  });

  it('reads a server that declared no kind at all', async () => {
    const states = new DeviceStates(stub(null).client);
    await states.read();

    expect(states.asked).toBe(true);
    expect(states.kinds).toEqual([]);
  });
});

describe('one request for one piece of data', () => {
  it('joins a second read to the one in flight', async () => {
    const asked = stub([printer]);
    const states = new DeviceStates(asked.client);

    await Promise.all([states.read(), states.read()]);

    expect(asked.calls).toBe(1);
  });
});

describe('a server that cannot answer', () => {
  it('keeps the fault and leaves every screen its own rule', async () => {
    const refusing: DeviceStatesClient = {
      getDeviceStateVocabulary: () =>
        Promise.reject(new StubApiError('core.api/INTERNAL')),
    };
    const states = new DeviceStates(refusing);

    await states.read();

    expect(states.asked).toBe(true);
    expect(states.error).toBeInstanceOf(Error);
    expect(states.transitionsOf(['printer'])).toEqual([]);
  });
});
