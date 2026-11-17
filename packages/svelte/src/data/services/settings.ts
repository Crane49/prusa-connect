// Moved from frontend/src/lib/services/settings.ts, 2026-09-11
//
// The requests the settings screen makes. A service names the SDK operation
// and its parameters and holds no state (svelte-data.md).
import type { PrusaConnect } from '@prusa/connect';

type CredentialPage = Awaited<ReturnType<PrusaConnect['listCredentials']>>;

/** One API credential as the listing reports it, without the token. */
export type ApiCredential = NonNullable<CredentialPage['items']>[number];

/** A credential as it is answered once, with the token in it. */
export type MintedCredential = Awaited<
  ReturnType<PrusaConnect['createCredential']>
>;

/** The configured instances, with the `ui` object on the ones that carry one. */
export type ResourcePage = Awaited<ReturnType<PrusaConnect['listResources']>>;

export type ResourceInstance = NonNullable<ResourcePage['items']>[number];

/** What the credentials store needs of the SDK, and nothing more. */
export type CredentialsClient = Pick<
  PrusaConnect,
  'listCredentials' | 'createCredential' | 'revokeCredential'
>;

/**
 * What a credential may use of its person's grants. Each scope allows what
 * the one before it allows, so a credential names one.
 */
export type CredentialScope = 'read' | 'operate' | 'administer';

/** Every scope, from the narrowest up. */
export const CREDENTIAL_SCOPES: readonly CredentialScope[] = [
  'read',
  'operate',
  'administer',
];

/** A pairing an app asked for, as the person deciding it reads it. */
export type AppPairing = Awaited<ReturnType<PrusaConnect['getAppPairing']>>;

/** What the pairing store needs of the SDK, and nothing more. */
export type AppPairingClient = Pick<
  PrusaConnect,
  'getAppPairing' | 'approveAppPairing' | 'denyAppPairing'
>;

/** What the modules store needs of the SDK, and nothing more. */
export type ModulesClient = Pick<PrusaConnect, 'listResources'>;

/** The caller's own credentials. An empty subject means whoever is signed in. */
export const readCredentials = (
  client: CredentialsClient,
): Promise<CredentialPage> => client.listCredentials();

const DAY_MS = 24 * 60 * 60 * 1000;

/**
 * Mints one. `days` sets how long the server keeps accepting it, and without
 * it the server chooses. The answer carries the token, and it is the only time this
 * server can show it: it keeps a hash and nothing else.
 */
export const mintCredential = (
  client: CredentialsClient,
  name: string,
  days?: number,
  scope: CredentialScope = 'read',
): Promise<MintedCredential> =>
  client.createCredential({
    body: {
      name,
      scopes: [scope],
      ...(days === undefined
        ? {}
        : { expires_at: new Date(Date.now() + days * DAY_MS).toISOString() }),
    },
  });

/**
 * Revokes one. `revoke-credential` is
 * `DELETE /api/v1/auth/credentials/{credential}`, and whatever was holding
 * that token stops being able to reach this server.
 */
export const revokeCredential = (
  client: CredentialsClient,
  credential: string,
): Promise<void> => client.revokeCredential({ path: { credential } });

/** What this server has installed, module or not. */
export const readResources = (
  client: ModulesClient,
  signal?: AbortSignal,
): Promise<ResourcePage> => client.listResources({}, signal);

/** Reads the pairing an app shows this code for. */
export const readAppPairing = (
  client: AppPairingClient,
  userCode: string,
): Promise<AppPairing> =>
  client.getAppPairing({ path: { user_code: userCode } });

/**
 * Approves a pairing with the scopes named, which are the ones the app asked
 * for or fewer. The app collects its credential on its next poll.
 */
export const approveAppPairing = (
  client: AppPairingClient,
  userCode: string,
  scopes: readonly CredentialScope[],
): Promise<void> =>
  client.approveAppPairing({
    path: { user_code: userCode },
    body: { scopes: [...scopes] },
  });

/** Refuses the app its credential. */
export const denyAppPairing = (
  client: AppPairingClient,
  userCode: string,
): Promise<void> => client.denyAppPairing({ path: { user_code: userCode } });
