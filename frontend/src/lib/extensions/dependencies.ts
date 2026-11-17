// The modules one extension depends on, and the installed ones that depend
// on it, as the Extensions page draws them (ADR-20261027-e6k rules 28 and
// 29).
//
// The keys are Composer's. A module requires another by its namespace and a
// constraint on its releases, suggests one with a reason, and conflicts with
// releases of one. The resources listing says, for each required and
// conflicting module, the release installed and whether the entry is met,
// and lists the installed modules that require this one.
//
// Plain functions over plain data, so a test computes them without a screen.
import type { components } from '@prusa/connect';

import type { ResourceInstance } from '@prusa/connect-ui/data';

import type { ExtensionRow } from './rows';

export type ModuleRequirement = components['schemas']['ResourceRequirement'];
export type ModuleDependant = components['schemas']['ModuleDependant'];
export type ModuleReason = components['schemas']['ModuleReason'];
export type ModuleOutdated = components['schemas']['ModuleOutdated'];

export interface ExtensionDependencies {
  /** The constraint it puts on the core, and empty for none. */
  readonly core: string;
  /** The modules it cannot run without. Capabilities are left out. */
  readonly requires: readonly ModuleRequirement[];
  /** The modules it works better with, each with the reason. */
  readonly suggests: readonly ModuleRequirement[];
  /** The releases of other modules it refuses to run beside. */
  readonly conflicts: readonly ModuleRequirement[];
  /** The installed modules that require it. */
  readonly requiredBy: readonly ModuleDependant[];
}

const modulesOnly = (
  rows: readonly ModuleRequirement[] | null | undefined,
): ModuleRequirement[] =>
  (rows ?? []).filter((row) => (row.module ?? '') !== '');

/** The core constraint among a require list, and empty for none. */
export const coreOf = (
  rows: readonly ModuleRequirement[] | null | undefined,
): string => (rows ?? []).find((row) => (row.core ?? '') !== '')?.core ?? '';

/** The dependencies of an installed module. */
export const dependenciesOfInstance = (
  instance: ResourceInstance | undefined,
): ExtensionDependencies => ({
  core: coreOf(instance?.module?.requires),
  requires: modulesOnly(instance?.module?.requires),
  suggests: modulesOnly(instance?.module?.suggests),
  conflicts: modulesOnly(instance?.module?.conflicts),
  requiredBy: instance?.module?.required_by ?? [],
});

/** Whether there is anything to draw. */
export const hasDependencies = (data: ExtensionDependencies): boolean =>
  data.core !== '' ||
  data.requires.length > 0 ||
  data.suggests.length > 0 ||
  data.conflicts.length > 0 ||
  data.requiredBy.length > 0;

/** The installed modules that require a row's module. */
export const requiredByOf = (row: ExtensionRow): readonly ModuleDependant[] =>
  row.instance?.module?.required_by ?? [];

/**
 * The short name of a module, the last part of its namespace, for a line a
 * person reads: com.github.aux.acme.planner is planner.
 */
export const shortNameOf = (namespace: string): string =>
  namespace.slice(namespace.lastIndexOf('.') + 1);

/** How one required module stands on this server. */
export type DependencyStanding = 'met' | 'missing' | 'off' | 'outOfRange';

/** How one required module stands. */
export const standingOf = (row: ModuleRequirement): DependencyStanding => {
  if ((row.installed ?? '') === '') return 'missing';
  if (row.met === true) return 'met';

  return row.state === 'disabled' ? 'off' : 'outOfRange';
};

/** How one conflict stands: clear, or met by a release installed here. */
export type ConflictStanding = 'clear' | 'conflicting';

/**
 * How one conflict stands. The server reports it met when the module is
 * absent or its release is outside the constraint.
 */
export const conflictStandingOf = (row: ModuleRequirement): ConflictStanding =>
  row.met === false && (row.installed ?? '') !== '' ? 'conflicting' : 'clear';

/** The outdated row of one module, and undefined when the list does not name it. */
export const outdatedOf = (
  rows: readonly ModuleOutdated[] | undefined,
  namespace: string | undefined,
): ModuleOutdated | undefined =>
  rows?.find((row) => row.namespace === (namespace ?? ''));
