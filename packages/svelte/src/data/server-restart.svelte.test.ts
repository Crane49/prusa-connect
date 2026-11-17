// The restart store: the request, then the wait for the server to come back in
// the state asked for.
import { describe, expect, it, vi } from 'vitest';

import { ServerRestart } from './server-restart.svelte';
import type { ServerRestartClient } from './services/maintenance';
import { StubApiError } from './stubs';

type Answer = () => Promise<{ safe_mode?: unknown }>;

const SAFE = {
  safe_mode: { reason: 'restart', since: '2026-10-30T10:00:00Z' },
};
const down: Answer = () => Promise.reject(new TypeError('connection refused'));
const up: Answer = () => Promise.resolve({});
const safe: Answer = () => Promise.resolve(SAFE);

/** A client whose reads answer from a script, the last answer repeating. */
const harness = (
  answers: Answer[],
  restart: () => Promise<unknown> = () => Promise.resolve({}),
  deadlineMs = 90_000,
  onSleep: () => void = () => undefined,
) => {
  let at = 0;
  const reads = [...answers];
  const restartServer = vi.fn(restart);
  const reload = vi.fn();
  const client = {
    restartServer,
    getUiSettings: () => (reads.length > 1 ? reads.shift()! : reads[0]!)(),
  } as unknown as ServerRestartClient;
  const store: ServerRestart = new ServerRestart(client, {
    deadlineMs,
    intervalMs: 1000,
    now: () => at,
    sleep: (ms) => {
      at += ms;
      onSleep();

      return Promise.resolve();
    },
    reload,
  });

  return { store, restartServer, reload };
};

describe('ServerRestart', () => {
  it('safe: keeps waiting through failures and answers without safe_mode, reloads at the first with it', async () => {
    const { store, restartServer, reload } = harness([up, down, up, safe]);
    const done = store.restart('safe');
    expect(store.phase).toBe('sending');
    await done;

    expect(restartServer).toHaveBeenCalledWith({ body: { mode: 'safe' } });
    expect(reload).toHaveBeenCalledTimes(1);
  });

  it('safe: does not reload while the server answers without safe_mode', async () => {
    const { store, reload } = harness([up], undefined, 5000);
    await store.restart('safe');

    expect(reload).not.toHaveBeenCalled();
    expect(store.phase).toBe('timeout');
  });

  it('normal: waits out safe_mode and reloads at the first answer without it', async () => {
    const { store, restartServer, reload } = harness([safe, safe, down, up]);
    await store.restart('normal');

    expect(restartServer).toHaveBeenCalledWith({ body: { mode: 'normal' } });
    expect(reload).toHaveBeenCalledTimes(1);
  });

  it('times out without reloading when the server never reaches the state', async () => {
    const { store, reload } = harness([down], undefined, 5000);
    await store.restart('safe');

    expect(store.phase).toBe('timeout');
    expect(reload).not.toHaveBeenCalled();
  });

  it('a refused request fails at once, with no wait after the state check', async () => {
    const refusal = new StubApiError('core.resource/UPGRADE_CONFLICT', 409);
    const reads = vi.fn();
    const { store, reload } = harness([reads], () => Promise.reject(refusal));
    await store.restart('safe');

    expect(store.phase).toBe('failed');
    expect(store.error).toBe(refusal);
    expect(store.refusal).toBe('upgrade-busy');
    expect(reads).toHaveBeenCalledTimes(1);
    expect(reload).not.toHaveBeenCalled();
  });

  it('names a 403 as forbidden and any other failure as none', async () => {
    const forbidden = harness([up], () =>
      Promise.reject(new StubApiError('core.auth/FORBIDDEN', 403)),
    );
    await forbidden.store.restart('safe');
    expect(forbidden.store.refusal).toBe('forbidden');

    const broken = harness([up], () =>
      Promise.reject(new StubApiError('x', 500)),
    );
    await broken.store.restart('safe');
    expect(broken.store.phase).toBe('failed');
    expect(broken.store.refusal).toBeUndefined();
  });

  it('ignores a second restart while one is waiting', async () => {
    let again: Promise<void> | undefined;
    const ctx: { store?: ServerRestart } = {};
    const h = harness([up, safe], undefined, 90_000, () => {
      again ??= ctx.store?.restart('safe');
    });
    ctx.store = h.store;
    await h.store.restart('safe');
    await again;

    expect(h.restartServer).toHaveBeenCalledTimes(1);
  });

  it('sends nothing when the server is already in the state asked for', async () => {
    const inSafe = harness([safe]);
    await inSafe.store.restart('safe');
    const outside = harness([up]);
    await outside.store.restart('normal');

    for (const h of [inSafe, outside]) {
      expect(h.restartServer).not.toHaveBeenCalled();
      expect(h.reload).not.toHaveBeenCalled();
      expect(h.store.phase).toBe('idle');
    }
  });

  it('cancel stops the wait: no more reads and no reload', async () => {
    const ctx: { store?: ServerRestart } = {};
    const h = harness([up, safe], undefined, 90_000, () => {
      ctx.store?.cancel();
    });
    ctx.store = h.store;
    await h.store.restart('safe');

    expect(h.reload).not.toHaveBeenCalled();
    expect(h.store.phase).toBe('idle');
  });
});
