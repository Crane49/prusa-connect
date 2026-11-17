// One person's access to one printer, as an administrator sees and changes it.
//
// The server holds grants as tuples, and a grant only adds: a printer role
// below a person's organization role changes nothing, and a block removes
// every level whatever its source (internal/authz/project.go). This module
// turns the tuples into one state a row can draw without contradicting the
// server, and turns a choice back into the grant writes that make it true.

/** The role a person holds over the whole organization. `''` is none. */
export type OrgRole = 'admin' | 'operator' | 'viewer' | '';

/** A role written on one printer. `manager` is shown as Administrator. */
export type DeviceRole = 'viewer' | 'operator' | 'manager';

/** What the role select offers: a printer role, or the organization's. */
export type AccessChoice = DeviceRole | 'org';

/** The relations a person can hold on a printer, as the grants list them. */
export type DirectRelation = DeviceRole | 'owner' | 'blocked';

/** Why a row draws a sentence instead of controls. */
export type AccessLock = 'orgAdmin' | 'self' | 'owner';

/**
 * Why one choice cannot be picked.
 *
 * - `inOrg`: the organization role already gives this much
 * - `aboveCaller`: the server refuses a grant of more than the caller holds
 * - `noOrgRole`: following the organization would mean no access while the
 *   switch says on
 */
export type ChoiceBlock = 'inOrg' | 'aboveCaller' | 'noOrgRole';

export interface AccessOption {
  readonly value: AccessChoice;
  readonly blocked: ChoiceBlock | undefined;
}

export type AccessState =
  | { readonly kind: 'locked'; readonly lock: AccessLock }
  | {
      readonly kind: 'editable';
      /** The person can use the printer. */
      readonly on: boolean;
      readonly choice: AccessChoice;
      /** The chosen printer role gives nothing the organization role does not. */
      readonly inert: boolean;
      readonly options: readonly AccessOption[];
      /** Set when the server would refuse to block this person for this caller. */
      readonly switchBlocked: 'aboveCaller' | undefined;
    };

export interface AccessStep {
  readonly op: 'write' | 'withdraw';
  readonly relation: DirectRelation;
}

const DEVICE_ROLES: readonly DeviceRole[] = ['viewer', 'operator', 'manager'];

export const ACCESS_CHOICES: readonly AccessChoice[] = ['org', ...DEVICE_ROLES];

const DEVICE_LEVEL: Readonly<Record<DeviceRole, number>> = {
  viewer: 1,
  operator: 2,
  manager: 3,
};

const ORG_LEVEL: Readonly<Record<OrgRole, number>> = {
  '': 0,
  viewer: 1,
  operator: 2,
  admin: 3,
};

/** The organization role an account reports, narrowed to the known ones. */
export const orgRoleOf = (role: string | undefined): OrgRole =>
  role === 'admin' || role === 'operator' || role === 'viewer' ? role : '';

/** Keeps the relations this module knows, from a grants listing. */
export const directRelationOf = (
  relation: string,
): DirectRelation | undefined =>
  relation === 'viewer' ||
  relation === 'operator' ||
  relation === 'manager' ||
  relation === 'owner' ||
  relation === 'blocked'
    ? relation
    : undefined;

/**
 * How much of the role ladder a caller holds on one printer, read from the
 * actions the server listed for them there.
 */
export const callerLevelOf = (
  actions: readonly string[] | null | undefined,
): number => {
  if (actions?.includes('administer')) return 3;
  if (actions?.includes('operate')) return 2;
  if (actions?.includes('observe')) return 1;

  return 0;
};

const isDeviceRole = (relation: DirectRelation): relation is DeviceRole =>
  relation !== 'owner' && relation !== 'blocked';

const directRole = (
  direct: readonly DirectRelation[],
): DeviceRole | undefined =>
  direct
    .filter(isDeviceRole)
    .reduce<DeviceRole | undefined>(
      (best, role) =>
        best === undefined || DEVICE_LEVEL[role] > DEVICE_LEVEL[best]
          ? role
          : best,
      undefined,
    );

const blockOf = (
  choice: AccessChoice,
  orgLevel: number,
  callerLevel: number,
): ChoiceBlock | undefined => {
  if (choice === 'org') return orgLevel === 0 ? 'noOrgRole' : undefined;
  if (DEVICE_LEVEL[choice] > callerLevel) return 'aboveCaller';
  if (DEVICE_LEVEL[choice] <= orgLevel) return 'inOrg';

  return undefined;
};

/**
 * The row state of one person on one printer.
 *
 * `callerLevel` is what the person changing it holds there (`callerLevelOf`).
 */
export const accessStateOf = (
  orgRole: OrgRole,
  direct: readonly DirectRelation[],
  isSelf: boolean,
  callerLevel: number,
): AccessState => {
  if (orgRole === 'admin') return { kind: 'locked', lock: 'orgAdmin' };
  if (isSelf) return { kind: 'locked', lock: 'self' };
  if (direct.includes('owner')) return { kind: 'locked', lock: 'owner' };

  const orgLevel = ORG_LEVEL[orgRole];
  const role = directRole(direct);
  const roleLevel = role === undefined ? 0 : DEVICE_LEVEL[role];
  const level = Math.max(orgLevel, roleLevel);

  return {
    kind: 'editable',
    on: !direct.includes('blocked') && level > 0,
    choice: role ?? 'org',
    inert: role !== undefined && roleLevel <= orgLevel,
    options: ACCESS_CHOICES.map((value) => ({
      value,
      blocked: blockOf(value, orgLevel, callerLevel),
    })),
    switchBlocked: level > callerLevel ? 'aboveCaller' : undefined,
  };
};

// Writes go first and withdrawals after, so a sequence that stops partway
// leaves the person with what they had or more, never locked out by halves.
const ordered = (steps: readonly AccessStep[]): AccessStep[] => [
  ...steps.filter((step) => step.op === 'write'),
  ...steps.filter((step) => step.op === 'withdraw'),
];

/**
 * The grant changes that turn the access switch on or off.
 *
 * Off writes a block and keeps the printer role, so the select still shows
 * what turning access back on gives. On lifts the block, and gives Viewer to a
 * person with no role anywhere, because on must mean they can use the printer.
 */
export const planAccessOn = (
  orgRole: OrgRole,
  direct: readonly DirectRelation[],
  on: boolean,
): AccessStep[] => {
  const blocked = direct.includes('blocked');
  if (!on) return blocked ? [] : [{ op: 'write', relation: 'blocked' }];

  const steps: AccessStep[] = [];
  if (ORG_LEVEL[orgRole] === 0 && directRole(direct) === undefined) {
    steps.push({ op: 'write', relation: 'viewer' });
  }
  if (blocked) steps.push({ op: 'withdraw', relation: 'blocked' });

  return ordered(steps);
};

/** The grant changes that make one choice of the select true. */
export const planAccessChoice = (
  direct: readonly DirectRelation[],
  choice: AccessChoice,
): AccessStep[] => {
  const held = direct.filter(isDeviceRole);
  const steps: AccessStep[] = held
    .filter((role) => role !== choice)
    .map((relation) => ({ op: 'withdraw', relation }));
  if (choice !== 'org' && !held.includes(choice)) {
    steps.push({ op: 'write', relation: choice });
  }

  return ordered(steps);
};
