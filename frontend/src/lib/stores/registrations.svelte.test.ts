// The waiting-devices store: what the stream changes in it, and what it
// forgets when the person signed in changes.
import type { StreamEvent } from '@prusa/connect';
import { describe, expect, it, vi } from 'vitest';

import { json, problem, stubClient } from '../../test/stub';
import { Registrations } from './registrations.svelte';

const waiting = (code: string) => ({
  code,
  fingerprint: `fp-${code}`,
  model: 'MK4',
  firmware: '6.0.0',
  serial: `serial-${code}`,
});

describe('the registrations reset', () => {
  it('forgets the waiting devices the last person could read', async () => {
    const { client } = stubClient({
      'GET /api/v1/registrations': json(200, { items: [waiting('a1')] }),
    });
    const store = new Registrations(client);
    await store.load();

    store.reset();

    expect(store.pending).toEqual([]);
    expect(store.loaded).toBe(false);
    expect(store.loading).toBe(false);
    expect(store.error).toBeUndefined();
  });

  it('drops an answer that was asked for before the reset', async () => {
    let answer: (() => void) | undefined;
    const { client } = stubClient({
      'GET /api/v1/registrations': () =>
        new Promise((resolve) => {
          answer = () => {
            resolve(json(200, { items: [waiting('a1')] }));
          };
        }),
    });
    const store = new Registrations(client);
    const read = store.load();
    await vi.waitFor(() => {
      expect(answer).toBeDefined();
    });

    store.reset();
    answer?.();
    await read;

    expect(store.pending).toEqual([]);
    expect(store.loaded).toBe(false);
  });

  // A change of person resets the store first, so the rows a failed read
  // keeps are the same person's.
  it('keeps the rows of the same person beside a failed read', async () => {
    let refused = false;
    const { client } = stubClient({
      'GET /api/v1/registrations': () =>
        refused
          ? problem(403, 'core.authz/REFUSED')
          : json(200, { items: [waiting('a1')] }),
    });
    const store = new Registrations(client);
    await store.load();

    refused = true;
    await store.load();

    expect(store.error).toBeDefined();
    expect(store.pending.map((row) => row.code)).toEqual(['a1']);
  });
});

const frame = (kind: string, payload: Record<string, unknown>): StreamEvent =>
  // The cast stands in for the narrowed union the SDK yields for one type.
  ({
    type: `com.prusa.connect.device.registration.${kind}`,
    id: '1',
    payload: { connector: 'xbuddy', ...payload },
    envelope: undefined,
  }) as unknown as StreamEvent;

const lost = {
  type: 'com.prusa.connect.stream.continuity.lost',
  id: '2',
  payload: {},
  envelope: undefined,
} as StreamEvent;

describe('the registrations as the stream moves them', () => {
  it('adds a code that arrives and removes it when somebody decides', async () => {
    const { client } = stubClient({
      'GET /api/v1/registrations': json(200, { items: [] }),
    });
    const store = new Registrations(client, () => 1000);
    await store.load();

    store.apply(frame('announced', waiting('a1')));
    store.apply(frame('announced', waiting('b2')));
    // A retry announces nothing new, and never doubles a row.
    store.apply(frame('announced', waiting('a1')));
    expect(store.pending.map((row) => row.code)).toEqual(['b2', 'a1']);

    store.apply(
      frame('approved', { code: 'a1', device_id: 'dev-1', model: 'MK4' }),
    );
    store.apply(frame('rejected', { code: 'b2' }));

    expect(store.pending).toEqual([]);
    expect(store.approvals).toEqual([
      {
        code: 'a1',
        deviceId: 'dev-1',
        model: 'MK4',
        serial: undefined,
        firmware: undefined,
        at: 1000,
      },
    ]);
  });

  it('keeps a refusal with its reason and never a fingerprint', () => {
    const { client } = stubClient({});
    const store = new Registrations(client, () => 5);

    store.apply(
      frame('refused', {
        reason: 'unknown_credentials',
        admission: 'approval',
        fingerprint_digest: 'abc',
        model: 'MK4',
      }),
    );
    // A reason this page does not know is not drawn as one it does.
    store.apply(frame('refused', { reason: 'something_else' }));

    expect(store.refusals).toEqual([
      {
        reason: 'unknown_credentials',
        admission: 'approval',
        model: 'MK4',
        firmware: undefined,
        digest: 'abc',
        at: 5,
      },
    ]);
  });

  it('forgets a refusal once a code is announced or approved after it', async () => {
    const { client } = stubClient({
      'POST /api/v1/registrations/a2/approve': json(200, {
        ...waiting('a2'),
        approved: true,
        connector: 'xbuddy',
        device_id: 'dev-2',
      }),
    });
    const store = new Registrations(client, () => 5);
    const refusal = frame('refused', {
      reason: 'unknown_credentials',
      admission: 'approval',
      fingerprint_digest: 'abc',
    });

    store.apply(refusal);
    store.apply(frame('announced', waiting('a1')));
    expect(store.refusals).toEqual([]);

    // A machine still refused is refused again on its next call.
    store.apply(refusal);
    expect(store.refusals).toHaveLength(1);

    store.apply(frame('approved', { code: 'a1', device_id: 'dev-1' }));
    expect(store.refusals).toEqual([]);

    store.apply(refusal);
    await store.approve('a2');
    expect(store.refusals).toEqual([]);
  });

  it('records the device the approval answered, once, beside the frame about it', async () => {
    const { client } = stubClient({
      'GET /api/v1/registrations': json(200, { items: [waiting('a1')] }),
      'POST /api/v1/registrations/a1/approve': json(200, {
        ...waiting('a1'),
        approved: true,
        connector: 'xbuddy',
        device_id: 'dev-1',
      }),
    });
    const store = new Registrations(client, () => 7);
    await store.load();

    await store.approve('a1');
    store.apply(frame('approved', { code: 'a1', device_id: 'dev-1' }));

    expect(store.pending).toEqual([]);
    expect(store.approvals.map((one) => [one.code, one.deviceId])).toEqual([
      ['a1', 'dev-1'],
    ]);
    expect(store.approvals[0]?.serial).toBe('serial-a1');
  });

  it('keeps the row and the refusal when an approval is refused', async () => {
    const { client } = stubClient({
      'GET /api/v1/registrations': json(200, { items: [waiting('a1')] }),
      'POST /api/v1/registrations/a1/approve': problem(
        403,
        'core.authz/REFUSED',
      ),
    });
    const store = new Registrations(client);
    await store.load();

    await store.approve('a1');

    expect(store.error).toBeDefined();
    expect(store.pending.map((row) => row.code)).toEqual(['a1']);
    expect(store.approvals).toEqual([]);
  });

  it('reads the list again when a frame arrived during a read, and when the stream lost its place', async () => {
    let answer: (() => void) | undefined;
    let reads = 0;
    const { client } = stubClient({
      'GET /api/v1/registrations': () => {
        reads += 1;
        if (reads > 1) return json(200, { items: [waiting('a1')] });

        return new Promise((resolve) => {
          answer = () => {
            resolve(json(200, { items: [] }));
          };
        });
      },
    });
    const store = new Registrations(client);
    const read = store.load();
    await vi.waitFor(() => {
      expect(answer).toBeDefined();
    });

    // The first answer was written before the code arrived.
    store.apply(frame('announced', waiting('a1')));
    answer?.();
    await read;

    expect(reads).toBe(2);
    expect(store.pending.map((row) => row.code)).toEqual(['a1']);

    store.apply(lost);
    await vi.waitFor(() => {
      expect(reads).toBe(3);
    });
  });

  it('hears the source it watches until it stops', () => {
    const { client } = stubClient({});
    const store = new Registrations(client);
    let listener: ((event: StreamEvent) => void) | undefined;
    const stop = store.watch({
      subscribe: (next) => {
        listener = next;

        return () => {
          listener = undefined;
        };
      },
    });

    listener?.(frame('announced', waiting('a1')));
    expect(store.pending.map((row) => row.code)).toEqual(['a1']);

    stop();
    expect(listener).toBeUndefined();
  });
});
