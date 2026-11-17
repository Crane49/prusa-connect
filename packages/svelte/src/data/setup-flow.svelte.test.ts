// The first-run wizard's conversation with the server (ADR-20261015-ejz): the code is
// checked by reading the configuration, the confirm waits for the restart, and
// a start that failed brings the person back to their answers.
import { describe, expect, it } from 'vitest';

import type { SetupWizardClient } from './services/auth';
import { SetupFlow } from './setup-flow.svelte';
import { StubApiError } from './stubs';

type State = Awaited<ReturnType<SetupWizardClient['getSetup']>>;

const pendingState = (extra: Partial<State> = {}): State => ({
  state: 'pending',
  version: '0.1.0',
  issuer: false,
  modules: [],
  steps: [],
  ...extra,
});

/** A server whose answers a test scripts, one per call. */
const scripted = (opts: {
  health?: (() => Promise<unknown>)[];
  setup?: (() => Promise<State>)[];
  record?: () => Promise<unknown>;
  contract?: () => Promise<unknown>;
  storage?: () => Promise<unknown>;
}): SetupWizardClient => {
  const health = [...(opts.health ?? [])];
  const setup = [...(opts.setup ?? [])];

  return {
    getHealth: () => (health.shift() ?? (() => Promise.resolve({})))(),
    getSetup: () =>
      (setup.shift() ?? (() => Promise.resolve(pendingState())))(),
    recordSetup: () =>
      (opts.record ?? (() => Promise.resolve({ restarting: true })))(),
    getSetupConfiguration: () =>
      (opts.contract ?? (() => Promise.resolve({ keys: [] })))(),
    checkSetupStorage: () =>
      (
        opts.storage ??
        (() => Promise.resolve({ root: '/s', exists: true, writable: true }))
      )(),
  } as unknown as SetupWizardClient;
};

/** A clock that moves one second per pause, so a wait never takes real time. */
const fastClock = (): {
  sleep: (ms: number) => Promise<void>;
  now: () => number;
} => {
  let at = 0;

  return {
    now: () => at,
    sleep: (ms: number) => {
      at += ms;

      return Promise.resolve();
    },
  };
};

const decision = { code: 'K7QD-3M9X-TP2A', mode: 'open' as const };

describe('the setup code', () => {
  it('is accepted when the configuration can be read with it', async () => {
    const flow = new SetupFlow(scripted({}));

    expect(await flow.readContract('K7QD-3M9X-TP2A')).toBe(true);
    expect(flow.contract).toEqual({ keys: [] });
    expect(flow.readError).toBeUndefined();
  });

  it('keeps the refusal as it was thrown, so the page can name the fault', async () => {
    const refused = new StubApiError('core.auth/SETUP_CODE_REFUSED', 403);
    const flow = new SetupFlow(
      scripted({ contract: () => Promise.reject(refused) }),
    );

    expect(await flow.readContract('WRONG')).toBe(false);
    expect(flow.readError).toBe(refused);
    expect(flow.contract).toBeUndefined();
  });
});

describe('the folder for uploads', () => {
  it('answers whether the server could write there', async () => {
    const flow = new SetupFlow(
      scripted({
        storage: () =>
          Promise.resolve({
            root: '/ro',
            exists: true,
            writable: false,
            problem: 'not_writable',
          }),
      }),
    );

    expect(await flow.checkStorage('code', '/ro')).toBe(false);
    expect(flow.storage?.problem).toBe('not_writable');
  });
});

describe('trying a sign-in provider', () => {
  const probe = {
    issuer: 'https://accounts.google.com',
    client_id: 'prusa-connect.apps.googleusercontent.com',
    client_secret: 'GOCSPX-secret',
    redirect_uri: 'http://localhost:8080/api/v1/auth/callback',
  };

  it('sends the code and the provider, and keeps what was found', async () => {
    const sent: unknown[] = [];
    const client = {
      checkSetupOidc: (request: unknown) => {
        sent.push(request);

        return Promise.resolve({
          issuer: probe.issuer,
          discovery: { status: 'passed' },
          client: { status: 'failed', detail: 'status 401: invalid_client' },
          redirect: { status: 'passed' },
        });
      },
    } as unknown as SetupWizardClient;
    const flow = new SetupFlow(client);

    await flow.checkOidc('K7QD-3M9X-TP2A', probe);

    expect(sent).toEqual([
      { header: { 'X-Setup-Code': 'K7QD-3M9X-TP2A' }, body: probe },
    ]);
    expect(flow.oidc?.client.status).toBe('failed');
    expect(flow.trying).toBe(false);

    flow.forgetOidc();
    expect(flow.oidc).toBeUndefined();
  });

  it('keeps a refused call apart from a failed step', async () => {
    const flow = new SetupFlow({
      checkSetupOidc: () => Promise.reject(new Error('the code is wrong')),
    } as unknown as SetupWizardClient);

    await flow.checkOidc('WRONG', probe);

    expect(flow.oidc).toBeUndefined();
    expect(flow.oidcError?.message).toBe('the code is wrong');
  });
});

describe('the confirm', () => {
  it('waits through a server that is down and ends ready', async () => {
    const down = (): Promise<unknown> =>
      Promise.reject(new Error('connection refused'));
    const flow = new SetupFlow(
      scripted({
        health: [down, down],
        setup: [
          () => Promise.resolve(pendingState()),
          () => Promise.resolve(pendingState()),
          () => Promise.resolve(pendingState({ state: 'done' })),
        ],
      }),
      fastClock(),
    );

    await flow.confirm(decision);

    expect(flow.phase).toBe('ready');
  });

  it('comes back to the answers with the failure when the start failed', async () => {
    const flow = new SetupFlow(
      scripted({
        setup: [
          () => Promise.resolve(pendingState()),
          () =>
            Promise.resolve(
              pendingState({
                last_error: {
                  message: 'listen tcp :80: permission denied',
                  at: '2026-10-15T11:00:00Z',
                },
              }),
            ),
        ],
      }),
      fastClock(),
    );

    await flow.confirm(decision);

    expect(flow.phase).toBe('failed');
    expect(flow.failure?.message).toContain('permission denied');
  });

  it('gives up after the deadline instead of waiting for ever', async () => {
    const down = (): Promise<unknown> =>
      Promise.reject(new Error('connection refused'));
    const flow = new SetupFlow(
      scripted({ health: Array.from({ length: 200 }, () => down) }),
      { ...fastClock(), deadlineMs: 10_000 },
    );

    await flow.confirm(decision);

    expect(flow.phase).toBe('timeout');
  });

  it('keeps the answers and the refusal when the confirm is refused', async () => {
    const refused = new StubApiError('core.request/INVALID', 422);
    const flow = new SetupFlow(
      scripted({ record: () => Promise.reject(refused) }),
    );

    await flow.confirm(decision);

    expect(flow.phase).toBe('editing');
    expect(flow.error).toBe(refused);
  });

  it('hands over to the page when the server moves to another address', async () => {
    const flow = new SetupFlow(scripted({}), fastClock());

    await flow.confirm(decision, false);

    expect(flow.phase).toBe('moved');
  });
});

describe('a failure from before the confirm', () => {
  it('is not taken for the failure of this one', async () => {
    const old = { message: 'an earlier failure', at: '2026-10-15T10:00:00Z' };
    const flow = new SetupFlow(
      scripted({
        setup: [
          () => Promise.resolve(pendingState({ last_error: old })),
          () =>
            Promise.resolve(pendingState({ state: 'done', last_error: old })),
        ],
      }),
      fastClock(),
    );

    await flow.confirm(decision);

    expect(flow.phase).toBe('ready');
    expect(flow.failure).toBeUndefined();
  });

  it('is told apart from a start that went back to the earlier settings', async () => {
    const fresh = {
      message: 'listen tcp :80: permission denied',
      at: '2026-10-15T11:00:00Z',
    };
    const flow = new SetupFlow(
      scripted({
        setup: [
          () => Promise.resolve(pendingState()),
          () =>
            Promise.resolve(pendingState({ state: 'done', last_error: fresh })),
        ],
      }),
      fastClock(),
    );

    await flow.confirm(decision);

    expect(flow.phase).toBe('ready');
    expect(flow.failure?.message).toContain('permission denied');
  });
});
