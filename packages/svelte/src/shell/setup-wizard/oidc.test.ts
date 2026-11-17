// A sign-in provider in the wizard: what it sends, what it refuses, and
// where its client secret is kept.
import { describe, expect, it } from 'vitest';

import type { RememberedStorage } from '../../remembered';
import { oidcAnswers, oidcErrors, stepIsComplete } from './checks';
import { DRAFT_KEY, SECRET_KEY, SetupDraft } from './setup-draft.svelte';

const memory = (): RememberedStorage & { map: Map<string, string> } => {
  const map = new Map<string, string>();

  return {
    map,
    getItem: (key) => map.get(key) ?? null,
    setItem: (key, value) => {
      map.set(key, value);
    },
  };
};

const CALLBACK = 'http://localhost:8080/api/v1/auth/callback';

const provider = (local = memory(), tab = memory()): SetupDraft => {
  const draft = new SetupDraft(local, tab);
  draft.mode = 'oidc';
  draft.sso({
    issuer: ' https://accounts.google.com ',
    clientId: 'prusa-connect.apps.googleusercontent.com',
    subject: '109876543210987654321',
  });

  return draft;
};

describe('a sign-in provider', () => {
  it('sends the client secret it was given', () => {
    const draft = provider();
    draft.oidcSecret = 'GOCSPX-secret';

    expect(oidcAnswers(draft, CALLBACK)).toEqual({
      'core.auth.issuer': 'https://accounts.google.com',
      'core.auth.client_id': 'prusa-connect.apps.googleusercontent.com',
      'core.auth.client_secret': 'GOCSPX-secret',
      'core.auth.redirect_uri': CALLBACK,
      'core.principal': '109876543210987654321',
    });
  });

  it('sends no client secret for a public client', () => {
    expect(oidcAnswers(provider(), CALLBACK)).not.toHaveProperty(
      'core.auth.client_secret',
    );
  });

  it('sends nothing when another way of signing in is chosen', () => {
    const draft = provider();
    draft.oidcSecret = 'GOCSPX-secret';
    draft.mode = 'required';

    expect(oidcAnswers(draft, CALLBACK)).toEqual({});
  });

  it('keeps the client secret in the tab and never in the lasting draft', () => {
    const local = memory();
    const tab = memory();
    provider(local, tab).oidcSecret = 'GOCSPX-secret';

    expect(local.map.get(DRAFT_KEY)).not.toContain('GOCSPX-secret');
    expect(tab.map.get(SECRET_KEY)).toContain('GOCSPX-secret');
    expect(new SetupDraft(local, tab).oidcSecret).toBe('GOCSPX-secret');
  });

  it('takes the subjects real providers hand out', () => {
    for (const subject of [
      'auth0|Owner',
      '0123456789abcdef'.repeat(4),
      'CiQwOGE4Njg0Yi1kYjg4LTRiNzMtOTBhOS0zY2QxNjYxZjU0NjYSBWxvY2Fs',
    ]) {
      const draft = provider();
      draft.sso({ subject });

      expect(oidcErrors(draft).subject).toBe('');
      expect(stepIsComplete('identity', draft, undefined)).toBe(true);
    }
  });

  it('refuses a subject no provider can hand out', () => {
    for (const subject of ['two words', 'x'.repeat(256)]) {
      const draft = provider();
      draft.sso({ subject });

      expect(oidcErrors(draft).subject).not.toBe('');
      expect(stepIsComplete('identity', draft, undefined)).toBe(false);
    }
  });

  it('lets the step go on without a client secret', () => {
    expect(stepIsComplete('identity', provider(), undefined)).toBe(true);
  });
});
