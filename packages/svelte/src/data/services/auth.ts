// Moved from frontend/src/lib/services/auth.ts, 2026-09-11
//
// The requests a screen makes about who is signed in. A service names the SDK
// operation and its parameters and holds no state (svelte-data.md).
import type { PrusaConnect } from '@prusa/connect';

import { MissingOperation } from '../fault';
import {
  type ModulePrincipal,
  type PrincipalClient,
  readPrincipal,
} from '../../domain/principal';

/** The two operations the first run is taken through. */
export type SetupClient = Pick<PrusaConnect, 'getSetup' | 'recordSetup'>;

/**
 * What the session needs of the SDK. A `PrusaConnect` satisfies it, and
 * naming the operations is what lets a test hand in a fake without writing
 * out every operation the core declares (svelte-data.md).
 *
 * The two first-run operations are optional because the first run is one
 * screen out of many: a host that serves its own way in, and a test about
 * another screen, hand a client that binds neither. The service below then
 * reports `MissingOperation` rather than throwing a `TypeError` in a store.
 */
export type AuthClient = PrincipalClient &
  Pick<PrusaConnect, 'passwordLogin' | 'logout'> &
  Partial<SetupClient> &
  Partial<Pick<PrusaConnect, 'setAccountPassword' | 'getAuthInfo'>>;

/**
 * What the first-run wizard needs of the SDK: the first run itself, the
 * configuration it asks for, and the probe it waits on while the server
 * restarts (ADR-20261015-ejz).
 */
export type SetupWizardClient = Pick<
  PrusaConnect,
  | 'getSetup'
  | 'recordSetup'
  | 'getSetupConfiguration'
  | 'checkSetupStorage'
  | 'checkSetupOidc'
  | 'createSetupCertificates'
  | 'getHealth'
>;

/** The configuration the wizard asks for, as `get-setup-configuration` answers it. */
export type SetupConfiguration = Awaited<
  ReturnType<PrusaConnect['getSetupConfiguration']>
>;

/** One configuration key the wizard may set. */
export type SetupKey = NonNullable<SetupConfiguration['keys']>[number];

/** Whether this server could keep uploads in a folder. */
export type SetupStorageCheck = Awaited<
  ReturnType<PrusaConnect['checkSetupStorage']>
>;

/** What trying a sign-in provider found, step by step. */
export type SetupOidcCheck = Awaited<
  ReturnType<PrusaConnect['checkSetupOidc']>
>;

/** The sign-in provider the wizard would configure. */
export type SetupOidcProbe = Parameters<
  PrusaConnect['checkSetupOidc']
>[0]['body'];

/** The start that failed after the wizard was last confirmed. */
export type SetupFailure = NonNullable<SetupState['last_error']>;

/** What the first-run page reads, as `get-setup` answers it. */
export type SetupState = Awaited<ReturnType<PrusaConnect['getSetup']>>;

/** One declared module, as the first-run page lists it. */
export type SetupModule = NonNullable<SetupState['modules']>[number];

/** What this server does about identity: it asks for a credential, or it does not. */
export type SetupMode = NonNullable<SetupState['mode']>;

/** The decision `record-setup` takes: the mode, the administrator, the modules. */
export type SetupDecision = Parameters<PrusaConnect['recordSetup']>[0]['body'];

/** What recording answered: the mode, and whether this browser was signed in. */
export type SetupRecorded = Awaited<ReturnType<PrusaConnect['recordSetup']>>;

/**
 * Signs in with a name and a password. `password-login` is
 * `POST /api/v1/auth/login`, and the answer sets the `pcl_session` cookie the
 * browser sends from then on. Nothing here reads that cookie: it is HttpOnly.
 */
export const signIn = async (
  client: AuthClient,
  username: string,
  password: string,
): Promise<void> => {
  await client.passwordLogin({ body: { username, password } });
};

/**
 * The identity provider this server sends a sign-in to, or `undefined` when it
 * takes a username and a password itself. `get-auth-info` is
 * `GET /api/v1/auth/info` and takes no credential. A client that does not bind
 * it, or a server that cannot answer, gets the password form: it is the one
 * way in that needs nothing from the server to be drawn.
 */
export const readRedirectProvider = async (
  client: AuthClient,
): Promise<string | undefined> => {
  const operation = client.getAuthInfo;
  if (operation === undefined) return undefined;
  try {
    const info = await operation.call(client, {});

    if (!info.redirect) return undefined;

    // The name the server gives the provider, such as its host, and the
    // adapter's type from a server that sends none.
    return info.provider_name !== undefined && info.provider_name !== ''
      ? info.provider_name
      : info.provider;
  } catch {
    return undefined;
  }
};

/**
 * Who this browser is signed in as. It is `GET /api/v1/principal` plus the
 * holdings, which is what `readPrincipal` reads. A 401 from it is the whole
 * of "show the sign-in".
 */
export const whoami = (client: AuthClient): Promise<ModulePrincipal> =>
  readPrincipal(client);

/**
 * Changes the signed-in person's own password.
 * `set-account-password` is `POST /api/v1/auth/accounts/{account}/password`.
 *
 * The server takes the current password when the account is the caller's own,
 * and refuses the change without it. A password reset for somebody else is a
 * different call, on the accounts store, and takes no current password.
 *
 * Every session opened under the old password ends, this browser's included,
 * so the caller reads who it is again after the change.
 */
export const changeOwnPassword = async (
  client: AuthClient,
  account: string,
  current: string,
  next: string,
): Promise<void> => {
  const operation = client.setAccountPassword;
  if (operation === undefined)
    throw new MissingOperation('set-account-password');

  await operation.call(client, {
    path: { account },
    body: { current_password: current, new_password: next },
  });
};

/** Ends the session. `logout` is `POST /api/v1/auth/logout`, which expires the cookie. */
export const signOut = (client: AuthClient): Promise<void> => client.logout();

/**
 * Whether this server has recorded what it does about identity, and what the
 * first-run page needs to offer the decision: the build, whether an issuer is
 * configured, and the declared modules. `get-setup` is `GET /api/v1/setup`,
 * and it takes no credential, because until the decision is there nobody
 * holds one.
 */
export const readSetup = (
  client: Partial<SetupClient>,
): Promise<SetupState> => {
  const operation = client.getSetup;
  if (operation === undefined) throw new MissingOperation('get-setup');

  return operation.call(client, {});
};

/**
 * The configuration the wizard asks for. `get-setup-configuration` is
 * `GET /api/v1/setup/configuration`, and it takes the one-time setup code in
 * `X-Setup-Code`, so a wrong code is refused before the person fills anything.
 */
export const readSetupConfiguration = (
  client: SetupWizardClient,
  code: string,
  signal?: AbortSignal,
): Promise<SetupConfiguration> =>
  client.getSetupConfiguration({ header: { 'X-Setup-Code': code } }, signal);

/**
 * Whether this server could keep uploads in a folder. `check-setup-storage`
 * is `POST /api/v1/setup/storage`, behind the setup code, and it creates
 * nothing.
 */
export const checkSetupStorage = (
  client: SetupWizardClient,
  code: string,
  root: string,
): Promise<SetupStorageCheck> =>
  client.checkSetupStorage({
    header: { 'X-Setup-Code': code },
    body: { root },
  });

/**
 * Tries a sign-in provider without signing anybody in. `check-setup-oidc` is
 * `POST /api/v1/setup/oidc`, behind the setup code. A refusal is a failed
 * step in the answer, not an error.
 */
export const checkSetupOidc = (
  client: SetupWizardClient,
  code: string,
  probe: SetupOidcProbe,
): Promise<SetupOidcCheck> =>
  client.checkSetupOidc({
    header: { 'X-Setup-Code': code },
    body: probe,
  });

/** The certificates the wizard made, and the authority that signed them. */
export type SetupCertificates = Awaited<
  ReturnType<PrusaConnect['createSetupCertificates']>
>;

/**
 * Makes the certificates the wizard encrypts with. `create-setup-certificates`
 * is `POST /api/v1/setup/certificates`, behind the setup code.
 */
export const createSetupCertificates = (
  client: SetupWizardClient,
  code: string,
  names: string[],
): Promise<SetupCertificates> =>
  client.createSetupCertificates({
    header: { 'X-Setup-Code': code },
    body: { names },
  });

/** Whether the server answers at all. `get-health` is `GET /api/v1/health`. */
export const probeHealth = async (
  client: SetupWizardClient,
  signal?: AbortSignal,
): Promise<void> => {
  await client.getHealth({}, signal);
};

/**
 * Records the decision. `record-setup` is `POST /api/v1/setup`. A mode of
 * `required` creates the first administrator and answers with their session
 * cookie, so the page continues as them; `open` takes no account. The server
 * refuses a second call, which is the 409 the page draws.
 */
export const recordSetup = (
  client: Partial<SetupClient>,
  decision: SetupDecision,
): Promise<SetupRecorded> => {
  const operation = client.recordSetup;
  if (operation === undefined) throw new MissingOperation('record-setup');

  return operation.call(client, { body: decision });
};
