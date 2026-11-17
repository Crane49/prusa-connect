import { flushSync } from 'svelte';
import { expect, test, vi } from 'vitest';

import {
  CLOCK_SKEW_FAULT,
  DEFAULT_POLL_MS,
  MAX_POLL_MS,
  PORTAL_UNAVAILABLE_FAULT,
  VERIFY_MS,
  PortalPairing,
  type PortalPairingService,
} from './portal-pairing.svelte';
import type { PortalPairingStatus } from './services/maintenance';

const pending: PortalPairingStatus = {
  state: 'pending',
  step: 'awaiting_confirmation',
  user_code: 'WDJB-MJHT',
  verification_uri: 'https://beta.prusa3d.com/device',
  verification_uri_complete:
    'https://beta.prusa3d.com/device?user_code=WDJB-MJHT',
  expires_at: '2026-10-27T09:15:00Z',
  interval_ms: 5000,
};

const refused = (code: string, status = 502): Error =>
  Object.assign(new Error(code), { code, status });

const serviceOf = (
  over: Partial<PortalPairingService> = {},
): PortalPairingService => ({
  getPortalPairingStatus: vi.fn(() =>
    Promise.resolve({ state: 'unpaired' as const }),
  ),
  startPortalPairing: vi.fn(() => Promise.resolve(pending)),
  refreshPortalPairing: vi.fn(() => Promise.resolve(pending)),
  disconnectPortal: vi.fn(() =>
    Promise.resolve({ state: 'unpaired' as const }),
  ),
  ...over,
});

/** Timers the test runs by hand, so the poll count is exact. */
const manualTimers = () => {
  const pendingTimers = new Map<number, { run: () => void; ms: number }>();
  let next = 0;

  return {
    setTimer: (run: () => void, ms: number) => {
      next += 1;
      pendingTimers.set(next, { run, ms });

      return next;
    },
    clearTimer: (timer: unknown) => {
      pendingTimers.delete(timer as number);
    },
    delays: () => [...pendingTimers.values()].map((timer) => timer.ms),
    fire: () => {
      const all = [...pendingTimers.entries()];
      pendingTimers.clear();
      for (const [, timer] of all) timer.run();
    },
    count: () => pendingTimers.size,
  };
};

test('starts unloaded and loads the unpaired state as the intro', async () => {
  const pairing = new PortalPairing(serviceOf());
  expect(pairing.loaded).toBe(false);
  expect(pairing.phase).toBe('loading');
  expect(pairing.status).toBeUndefined();

  await pairing.load();

  expect(pairing.loaded).toBe(true);
  expect(pairing.status).toEqual({ state: 'unpaired' });
  expect(pairing.phase).toBe('intro');
});

test('one load in flight is shared by every caller', async () => {
  const service = serviceOf();
  const pairing = new PortalPairing(service);

  await Promise.all([pairing.load(), pairing.load(), pairing.load()]);

  expect(service.getPortalPairingStatus).toHaveBeenCalledOnce();
});

test('the phases follow the server state, the step and the opened portal', async () => {
  let answer: PortalPairingStatus = pending;
  const pairing = new PortalPairing(
    serviceOf({ refreshPortalPairing: vi.fn(() => Promise.resolve(answer)) }),
  );
  await pairing.start();
  expect(pairing.phase).toBe('code');

  pairing.markOpened();
  expect(pairing.opened).toBe(true);
  expect(pairing.phase).toBe('confirm');

  answer = { ...pending, step: 'awaiting_approval' };
  await pairing.refresh();
  expect(pairing.phase).toBe('approval');

  for (const state of ['expired', 'denied', 'revoked'] as const) {
    answer = { state };
    await pairing.refresh();
    expect(pairing.phase).toBe(state);
    expect(pairing.opened).toBe(false);
  }

  answer = { state: 'paired', identity: { name: 'Ada' } };
  await pairing.load();
  await pairing.refresh();
  expect(pairing.phase).toBe('paired');
});

test('opening the portal is ignored when no code is on screen', async () => {
  const pairing = new PortalPairing(serviceOf());
  await pairing.load();

  pairing.markOpened();

  expect(pairing.opened).toBe(false);
  expect(pairing.phase).toBe('intro');
});

test('a wrong clock and an unreachable portal have their own phases', async () => {
  const service = serviceOf({
    startPortalPairing: vi.fn(() => Promise.reject(refused(CLOCK_SKEW_FAULT))),
  });
  const pairing = new PortalPairing(service);
  await pairing.load();

  await pairing.start();
  expect(pairing.phase).toBe('clock');
  expect(pairing.error?.code).toBe(CLOCK_SKEW_FAULT);

  vi.mocked(service.startPortalPairing).mockRejectedValueOnce(
    refused(PORTAL_UNAVAILABLE_FAULT),
  );
  await pairing.retry();
  expect(pairing.phase).toBe('unreachable');

  vi.mocked(service.startPortalPairing).mockRejectedValueOnce(
    Object.assign(new Error('Failed to fetch'), { name: 'NetworkError' }),
  );
  await pairing.retry();
  expect(pairing.phase).toBe('unreachable');

  vi.mocked(service.startPortalPairing).mockResolvedValueOnce(pending);
  await pairing.retry();
  expect(pairing.phase).toBe('code');
  expect(pairing.error).toBeUndefined();
});

test('an unreachable portal keeps a pending code on screen', async () => {
  const pairing = new PortalPairing(
    serviceOf({
      refreshPortalPairing: vi.fn(() =>
        Promise.reject(refused(PORTAL_UNAVAILABLE_FAULT)),
      ),
    }),
  );
  await pairing.start();

  await pairing.refresh();

  expect(pairing.phase).toBe('code');
  expect(pairing.error?.code).toBe(PORTAL_UNAVAILABLE_FAULT);
});

test('a start on a paired server shows the pairing instead of the refusal', async () => {
  const service = serviceOf({
    getPortalPairingStatus: vi.fn(() =>
      Promise.resolve({ state: 'paired' as const }),
    ),
    startPortalPairing: vi.fn(() =>
      Promise.reject(refused('core.resource/BETA_PORTAL_ALREADY_PAIRED', 409)),
    ),
  });
  const pairing = new PortalPairing(service);

  await pairing.start();

  expect(pairing.error).toBeUndefined();
  expect(pairing.phase).toBe('paired');
  expect(service.getPortalPairingStatus).toHaveBeenCalledOnce();
});

test('a click while an action runs does not send a second request', async () => {
  let finish: (status: PortalPairingStatus) => void = () => undefined;
  const service = serviceOf({
    startPortalPairing: vi.fn(
      () =>
        new Promise<PortalPairingStatus>((resolve) => {
          finish = resolve;
        }),
    ),
  });
  const pairing = new PortalPairing(service);

  const first = pairing.start();
  expect(pairing.busy).toBe('start');
  await pairing.start();
  await pairing.disconnect();
  finish(pending);
  await first;

  expect(service.startPortalPairing).toHaveBeenCalledOnce();
  expect(service.disconnectPortal).not.toHaveBeenCalled();
  expect(pairing.busy).toBeUndefined();
});

test('a disconnect the portal could not confirm still leaves the server unpaired', async () => {
  const pairing = new PortalPairing(
    serviceOf({
      getPortalPairingStatus: vi.fn(() =>
        Promise.resolve({ state: 'paired' as const }),
      ),
      disconnectPortal: vi.fn(() =>
        Promise.reject(
          refused('core.resource/BETA_PORTAL_REVOCATION_INCOMPLETE'),
        ),
      ),
    }),
  );
  await pairing.load();

  await pairing.disconnect();

  expect(pairing.status).toEqual({ state: 'unpaired' });
  expect(pairing.phase).toBe('intro');
  expect(pairing.error?.code).toBe(
    'core.resource/BETA_PORTAL_REVOCATION_INCOMPLETE',
  );
});

test('a watched pending pairing polls once per server interval', async () => {
  const timers = manualTimers();
  const service = serviceOf();
  const pairing = new PortalPairing(service, timers);
  const stop = pairing.watch();
  await pairing.load();
  expect(timers.count()).toBe(0);

  await pairing.start();
  expect(timers.delays()).toEqual([5000]);

  for (let poll = 1; poll <= 3; poll += 1) {
    timers.fire();
    await vi.waitFor(() => {
      expect(service.refreshPortalPairing).toHaveBeenCalledTimes(poll);
      expect(timers.count()).toBe(1);
    });
  }

  stop();
  expect(timers.count()).toBe(0);
  stop();
  expect(timers.count()).toBe(0);
});

test('the poll takes the interval the server names', async () => {
  const timers = manualTimers();
  const pairing = new PortalPairing(
    serviceOf({
      startPortalPairing: vi.fn(() =>
        Promise.resolve({ ...pending, interval_ms: 10_000 }),
      ),
    }),
    timers,
  );
  pairing.watch();

  await pairing.start();

  expect(pairing.pollDelayMs).toBe(10_000);
  expect(timers.delays()).toEqual([10_000]);
});

test('a failing poll waits twice as long each time, up to a minute', async () => {
  const timers = manualTimers();
  const service = serviceOf({
    refreshPortalPairing: vi.fn(() =>
      Promise.reject(refused(PORTAL_UNAVAILABLE_FAULT)),
    ),
  });
  const pairing = new PortalPairing(service, timers);
  pairing.watch();
  await pairing.start();

  const seen: number[] = [];
  for (let poll = 1; poll <= 6; poll += 1) {
    timers.fire();
    await vi.waitFor(() => {
      expect(service.refreshPortalPairing).toHaveBeenCalledTimes(poll);
      expect(timers.count()).toBe(1);
    });
    seen.push(...timers.delays());
  }

  expect(seen).toEqual([
    10_000,
    20_000,
    40_000,
    MAX_POLL_MS,
    MAX_POLL_MS,
    MAX_POLL_MS,
  ]);

  vi.mocked(service.refreshPortalPairing).mockResolvedValueOnce(pending);
  timers.fire();
  await vi.waitFor(() => {
    expect(timers.delays()).toEqual([DEFAULT_POLL_MS]);
  });
});

test('a watched paired server is checked at once, then every minute', async () => {
  const timers = manualTimers();
  const refresh = vi.fn(() =>
    Promise.resolve({
      state: 'paired' as const,
      access_suspended: true,
    }),
  );
  const pairing = new PortalPairing(
    serviceOf({
      getPortalPairingStatus: vi.fn(() =>
        Promise.resolve({ state: 'paired' as const }),
      ),
      refreshPortalPairing: refresh,
    }),
    timers,
  );
  pairing.watch();
  await pairing.load();

  expect(timers.delays()).toEqual([0]);
  timers.fire();
  await vi.waitFor(() => {
    expect(pairing.status?.access_suspended).toBe(true);
  });
  expect(refresh).toHaveBeenCalledTimes(1);
  expect(timers.delays()).toEqual([VERIFY_MS]);
});

test('nothing polls once the pairing ended', async () => {
  const timers = manualTimers();
  const pairing = new PortalPairing(
    serviceOf({
      refreshPortalPairing: vi.fn(() =>
        Promise.resolve({ state: 'revoked' as const }),
      ),
    }),
    timers,
  );
  pairing.watch();
  await pairing.start();

  timers.fire();
  await vi.waitFor(() => {
    expect(pairing.phase).toBe('revoked');
  });

  expect(timers.count()).toBe(0);
});

test('an unwatched pairing never polls', async () => {
  const timers = manualTimers();
  const pairing = new PortalPairing(serviceOf(), timers);

  await pairing.start();

  expect(timers.count()).toBe(0);
});

test('onPaired runs when a pending pairing finishes, never on load', async () => {
  const onPaired = vi.fn();
  const paired = { state: 'paired' as const };
  const pairing = new PortalPairing(
    serviceOf({
      getPortalPairingStatus: vi.fn(() => Promise.resolve(paired)),
      refreshPortalPairing: vi.fn(() => Promise.resolve(paired)),
    }),
    { onPaired },
  );

  await pairing.load();
  expect(onPaired).not.toHaveBeenCalled();

  await pairing.disconnect();
  await pairing.start();
  await pairing.refresh();
  await pairing.refresh();
  expect(onPaired).toHaveBeenCalledOnce();
});

test('a reset drops a late answer and stops the timer', async () => {
  const timers = manualTimers();
  let finish: (status: PortalPairingStatus) => void = () => undefined;
  const pairing = new PortalPairing(
    serviceOf({
      getPortalPairingStatus: vi.fn(
        () =>
          new Promise<PortalPairingStatus>((resolve) => {
            finish = resolve;
          }),
      ),
    }),
    timers,
  );
  pairing.watch();
  const loading = pairing.load();

  pairing.reset();
  finish(pending);
  await loading;

  expect(pairing.status).toBeUndefined();
  expect(pairing.loaded).toBe(false);
  expect(timers.count()).toBe(0);
});

test('a poll that changes nothing recomputes the phase once at most', async () => {
  const pairing = new PortalPairing(serviceOf());
  await pairing.start();

  let runs = 0;
  const cleanup = $effect.root(() => {
    $effect(() => {
      if (pairing.phase !== 'loading') runs += 1;
    });
  });
  flushSync();
  expect(runs).toBe(1);

  await pairing.refresh();
  flushSync();
  expect(runs).toBe(1);
  expect(pairing.phase).toBe('code');

  cleanup();
});

test('a disconnect refused by this server reads the pairing back', async () => {
  const service = serviceOf({
    getPortalPairingStatus: vi.fn(() =>
      Promise.resolve({ state: 'paired' as const }),
    ),
    disconnectPortal: vi.fn(() =>
      Promise.reject(refused('core.auth/ACCESS_DENIED', 403)),
    ),
  });
  const pairing = new PortalPairing(service);
  await pairing.load();

  await pairing.disconnect();

  await vi.waitFor(() => {
    expect(service.getPortalPairingStatus).toHaveBeenCalledTimes(2);
  });
  expect(pairing.status).toEqual({ state: 'paired' });
  expect(pairing.phase).toBe('paired');
});

test('a reset drops the answer of an action still on the way', async () => {
  let finish: (status: PortalPairingStatus) => void = () => undefined;
  const pairing = new PortalPairing(
    serviceOf({
      startPortalPairing: vi.fn(
        () =>
          new Promise<PortalPairingStatus>((resolve) => {
            finish = resolve;
          }),
      ),
    }),
  );
  const starting = pairing.start();

  pairing.reset();
  finish(pending);
  await starting;

  expect(pairing.status).toBeUndefined();
  expect(pairing.loaded).toBe(false);
  expect(pairing.busy).toBeUndefined();
});
