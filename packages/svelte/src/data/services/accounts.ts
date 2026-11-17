// The requests the People pane makes.
import type { PrusaConnect } from '@prusa/connect';

/** The local accounts, as `list-accounts` answers. */
export type AccountPage = Awaited<ReturnType<PrusaConnect['listAccounts']>>;

/** One account, with its role and the grants it was given. */
export type Account = NonNullable<AccountPage['items']>[number];

/** What `create-account` takes: a username, a password, a name and a role. */
export type NewAccount = Parameters<PrusaConnect['createAccount']>[0]['body'];

/** The three roles the document declares on `create-account`. */
export type AccountRoleName = NonNullable<NewAccount['role']>;

export const ACCOUNT_ROLES: readonly AccountRoleName[] = [
  'viewer',
  'operator',
  'admin',
];

/** What the accounts store needs of the SDK. */
export type AccountsClient = Pick<
  PrusaConnect,
  | 'listAccounts'
  | 'createAccount'
  | 'deleteAccount'
  | 'setAccountPassword'
  | 'setAccountRole'
>;

/** Every account on this server. An administrator is the only caller allowed. */
export const readAccounts = (client: AccountsClient): Promise<AccountPage> =>
  client.listAccounts();

/** Adds one person. The role decides what they may do from their first sign-in. */
export const createAccount = (
  client: AccountsClient,
  account: NewAccount,
): Promise<Account> => client.createAccount({ body: account });

/** Removes one person. They can no longer sign in; their prints are untouched. */
export const deleteAccount = (
  client: AccountsClient,
  account: string,
): Promise<void> => client.deleteAccount({ path: { account } });

/** The account id inside a principal subject. */
export const accountIdOf = (
  subject: string | undefined,
): string | undefined => {
  const prefix = 'user:';
  if (!subject?.startsWith(prefix)) return undefined;
  const id = subject.slice(prefix.length);

  return id === '' ? undefined : id;
};

/** Sets a new password on one account. */
export const setAccountPassword = (
  client: AccountsClient,
  account: string,
  password: string,
  current?: string,
): Promise<void> =>
  client.setAccountPassword({
    path: { account },
    body:
      current === undefined
        ? { new_password: password }
        : { current_password: current, new_password: password },
  });

/** Moves one account to another role. */
export const setAccountRole = (
  client: AccountsClient,
  account: string,
  role: AccountRoleName,
): Promise<void> =>
  client.setAccountRole({ path: { account }, body: { role } });
