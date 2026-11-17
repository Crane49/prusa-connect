// What the pairing page can do: read the pairing a code names, approve it with
// the scopes the person keeps, or deny it.
import { describe, expect, it } from 'vitest';

import { AppPairingReview } from './app-pairing.svelte';
import type { AppPairing, AppPairingClient } from './services/settings';
import { StubApiError } from './stubs';

const pairing: AppPairing = {
  app_name: 'kitchen display',
  client_address: '192.0.2.41',
  expires_at: '2026-10-24T12:10:00Z',
  scopes: ['read', 'operate'],
  user_code: 'BCDF-GHJK',
};

const stub = (refuse?: StubApiError) => {
  const approved: unknown[] = [];
  const denied: string[] = [];
  const read: string[] = [];

  const client: AppPairingClient = {
    getAppPairing: (request) => {
      read.push(request.path.user_code);
      if (refuse) return Promise.reject(refuse);

      return Promise.resolve(pairing);
    },
    approveAppPairing: (request) => {
      approved.push(request.body?.scopes);

      return Promise.resolve();
    },
    denyAppPairing: (request) => {
      denied.push(request.path.user_code);

      return Promise.resolve();
    },
  };

  return { client, approved, denied, read };
};

describe('AppPairingReview', () => {
  it('reads the pairing a code names once', async () => {
    const { client, read } = stub();
    const review = new AppPairingReview(client);

    await review.load(' BCDF-GHJK ');
    await review.load('BCDF-GHJK');

    expect(read).toEqual(['BCDF-GHJK']);
    expect(review.pairing?.app_name).toBe('kitchen display');
    expect(review.loaded).toBe(true);
  });

  it('approves with the scopes the person kept', async () => {
    const { client, approved } = stub();
    const review = new AppPairingReview(client);
    await review.load('BCDF-GHJK');

    await review.approve(['read']);

    expect(approved).toEqual([['read']]);
    expect(review.outcome).toBe('approved');
  });

  it('approves nothing with no scope', async () => {
    const { client, approved } = stub();
    const review = new AppPairingReview(client);
    await review.load('BCDF-GHJK');

    await review.approve([]);

    expect(approved).toEqual([]);
    expect(review.outcome).toBeUndefined();
  });

  it('denies', async () => {
    const { client, denied } = stub();
    const review = new AppPairingReview(client);
    await review.load('BCDF-GHJK');

    await review.deny();

    expect(denied).toEqual(['BCDF-GHJK']);
    expect(review.outcome).toBe('denied');
  });

  // A code that names nothing is a refusal the screen draws by its code.
  it('keeps the refusal of a code nothing answers', async () => {
    const { client } = stub(
      new StubApiError('core.auth/APP_PAIRING_MISSING', 404),
    );
    const review = new AppPairingReview(client);

    await review.load('ZZZZ-ZZZZ');

    expect(review.error).toBeDefined();
    expect(review.pairing).toBeUndefined();
    expect(review.loaded).toBe(true);
  });

  it('forgets everything on reset', async () => {
    const { client } = stub();
    const review = new AppPairingReview(client);
    await review.load('BCDF-GHJK');

    review.reset();

    expect(review.code).toBe('');
    expect(review.pairing).toBeUndefined();
  });
});
