// The rows of the two access lists: every person on one printer, and every
// printer for one person. Both are the same row, so both views say the same
// thing about the same grant.
import { accessKey, type Account } from '../../data';
import {
  accessStateOf,
  callerLevelOf,
  type Device,
  orgRoleOf,
} from '../../domain';
import { t } from '../../i18n';
import { roleLabelKey } from '../settings-people/presentation';
import type { AccessGrantsSource, AccessRow } from './types';

/** The name a person goes by, the username when they gave none. */
export const displayNameOf = (account: Account): string =>
  account.display_name.trim() === '' ? account.username : account.display_name;

/** The organization role in words, as People writes it. */
export const orgRoleLabel = (role: string | undefined): string =>
  t(roleLabelKey(role ?? ''));

/** Every person on one printer, in the order the server listed them. */
export const peopleRowsOf = (
  accounts: readonly Account[],
  grants: AccessGrantsSource,
  deviceId: string,
  callerActions: readonly string[] | undefined,
  self: string | undefined,
): AccessRow[] => {
  const callerLevel = callerLevelOf(callerActions);

  return accounts.map((account) => {
    const orgRole = orgRoleOf(account.role);
    const name = displayNameOf(account);

    return {
      key: accessKey(deviceId, account.subject),
      deviceId,
      subject: account.subject,
      primary: account.username,
      mono: true,
      // An administrator's role is the badge in place of the controls.
      details: [
        ...(name === account.username ? [] : [name]),
        ...(orgRole === 'admin' ? [] : [orgRoleLabel(account.role)]),
      ],
      name,
      orgRole,
      state: accessStateOf(
        orgRole,
        grants.direct(deviceId, account.subject),
        account.subject === self,
        callerLevel,
      ),
      busy: grants.busy(deviceId, account.subject),
    };
  });
};

/** Every printer on one page of the fleet, for one person. */
export const printerRowsOf = (
  devices: readonly Device[],
  grants: AccessGrantsSource,
  account: Account,
  self: string | undefined,
): AccessRow[] => {
  const orgRole = orgRoleOf(account.role);

  return devices.map((device) => ({
    key: accessKey(device.id, account.subject),
    deviceId: device.id,
    subject: account.subject,
    primary: device.name,
    mono: false,
    details: device.model === '' ? [] : [device.model],
    name: device.name,
    orgRole,
    state: accessStateOf(
      orgRole,
      grants.direct(device.id, account.subject),
      account.subject === self,
      callerLevelOf(device.actions),
    ),
    busy: grants.busy(device.id, account.subject),
  }));
};
