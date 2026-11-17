import { describe, expect, it } from 'vitest';

import type { TimeServerClient, TimeServerState } from './services/discovery';
import { StubApiError } from './stubs';
import { TimeServerSwitch } from './time-server.svelte';

const OFF: TimeServerState = { enabled: false, state: 'off', port: 123 };

const stubClient = (
  start: TimeServerState,
): {
  client: TimeServerClient;
  held: { answer: TimeServerState; written: boolean[]; refusal?: StubApiError };
} => {
  const held: {
    answer: TimeServerState;
    written: boolean[];
    refusal?: StubApiError;
  } = { answer: start, written: [] };
  const client: TimeServerClient = {
    getTimeServer: () => Promise.resolve(held.answer),
    setTimeServer: (request) => {
      held.written.push(request.body.enabled);
      if (held.refusal) return Promise.reject(held.refusal);

      held.answer = {
        enabled: request.body.enabled,
        state: request.body.enabled ? 'serving' : 'off',
        port: 123,
      };

      return Promise.resolve(held.answer);
    },
  };

  return { client, held };
};

describe('TimeServerSwitch', () => {
  it('starts unread', () => {
    const store = new TimeServerSwitch(stubClient(OFF).client);

    expect(store.loaded).toBe(false);
    expect(store.enabled).toBe(false);
  });

  it('reads the switch and the state', async () => {
    const store = new TimeServerSwitch(
      stubClient({
        enabled: true,
        state: 'failed',
        reason: 'listen udp :123: bind: permission denied',
        port: 123,
      }).client,
    );

    await store.load();

    expect(store.loaded).toBe(true);
    expect(store.enabled).toBe(true);
    expect(store.state).toBe('failed');
    expect(store.reason).toContain('permission denied');
  });

  it('shows a change once the server applied it', async () => {
    const { client, held } = stubClient(OFF);
    const store = new TimeServerSwitch(client);
    await store.load();

    await store.setEnabled(true);

    expect(held.written).toEqual([true]);
    expect(store.enabled).toBe(true);
    expect(store.state).toBe('serving');
    expect(store.saving).toBe(false);
  });

  it('keeps the switch the server holds after a refusal', async () => {
    const { client, held } = stubClient(OFF);
    held.refusal = new StubApiError('core.authz/ACCESS_DENIED');
    const store = new TimeServerSwitch(client);
    await store.load();

    await store.setEnabled(true);

    expect(store.enabled).toBe(false);
    expect(store.error?.code).toBe('core.authz/ACCESS_DENIED');
  });

  it('forgets everything on reset', async () => {
    const store = new TimeServerSwitch(stubClient(OFF).client);
    await store.load();

    store.reset();

    expect(store.loaded).toBe(false);
  });

  it('keeps a change when a read sent before it answers after it', async () => {
    let answerRead: (state: TimeServerState) => void = () => undefined;
    const client: TimeServerClient = {
      getTimeServer: () =>
        new Promise((resolve) => {
          answerRead = resolve;
        }),
      setTimeServer: (request) =>
        Promise.resolve({
          enabled: request.body.enabled,
          state: 'serving',
          port: 123,
        }),
    };
    const store = new TimeServerSwitch(client);

    const read = store.load();
    await store.setEnabled(true);
    answerRead(OFF);
    await read;

    expect(store.enabled).toBe(true);
    expect(store.state).toBe('serving');
  });
});
