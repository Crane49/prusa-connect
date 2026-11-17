import type { ResourceInstance } from '@prusa/connect-ui/data';
import {
  describeFault,
  type FaultText,
  formatList,
  type Translate,
} from '@prusa/connect-ui/i18n';

import { requiredByOf, shortNameOf } from './dependencies';
import type { ExtensionRow } from './rows';
import { stopText } from './stops';

/**
 * What a row's switch does. A module installed without approval is switched
 * on through the install question, never by writing `enabled`: written on, it
 * would start with nothing granted.
 */
export type SwitchState =
  | { readonly kind: 'install' }
  | { readonly kind: 'approve'; readonly version: string }
  | { readonly kind: 'toggle'; readonly on: boolean }
  | {
      readonly kind: 'locked';
      readonly on: boolean;
      readonly reason:
        'file' | 'access' | 'unapproved' | 'modules' | 'safeMode';
    }
  | {
      readonly kind: 'locked';
      readonly on: true;
      readonly reason: 'required';
      /** The installed modules that require it, by short name. */
      readonly names: readonly string[];
    }
  | { readonly kind: 'none' };

export const switchOf = (
  row: ExtensionRow,
  mayConfigure: boolean | undefined,
  modulesOn = true,
  safeMode = false,
): SwitchState => {
  const { instance } = row;

  if (instance === undefined) {
    if (!modulesOn || row.source !== 'local' || row.offer === undefined) {
      return { kind: 'none' };
    }

    return mayConfigure === false
      ? { kind: 'locked', on: false, reason: 'access' }
      : { kind: 'install' };
  }

  if (instance.source === 'file') {
    if (safeMode) return { kind: 'locked', on: false, reason: 'safeMode' };

    return { kind: 'locked', on: instance.enabled, reason: 'file' };
  }
  // The operator switched installed modules off, and the server starts none
  // of them whatever its flag says (ADR-20261019-h76).
  if (!modulesOn) {
    return {
      kind: 'locked',
      on: false,
      reason: safeMode ? 'safeMode' : 'modules',
    };
  }
  if (mayConfigure === false) {
    return { kind: 'locked', on: instance.enabled, reason: 'access' };
  }

  if (instance.module !== undefined && instance.module.asked === undefined) {
    return row.offer === undefined
      ? { kind: 'locked', on: false, reason: 'unapproved' }
      : { kind: 'approve', version: row.installedVersion ?? row.version };
  }

  // A module a switched on module requires cannot be switched off: the
  // operator switches off or removes the dependant first (ADR-20261027-e6k
  // rule 19). A dependant that is off holds nothing, as the server judges.
  const dependants = requiredByOf(row).filter((one) => one.enabled);
  if (instance.enabled && dependants.length > 0) {
    return {
      kind: 'locked',
      on: true,
      reason: 'required',
      names: dependants.map((one) => shortNameOf(one.module)),
    };
  }

  return { kind: 'toggle', on: instance.enabled };
};

/**
 * Whether a row's module can be removed from this page. Unknown access is left
 * to the server, which decides again. Not for a module config.yaml declares:
 * the server never writes that file, so it refuses the removal. Not for a
 * module another installed module requires, which stays until the dependant
 * is removed (ADR-20261027-e6k rule 19).
 */
export const removableOf = (
  row: ExtensionRow,
  mayConfigure: boolean | undefined,
): boolean =>
  mayConfigure !== false &&
  row.state !== 'available' &&
  (row.namespace ?? '') !== '' &&
  row.instance?.source !== 'file' &&
  requiredByOf(row).length === 0;

export type Observed =
  | 'running'
  | 'degraded'
  | 'starting'
  | 'retrying'
  | 'stopped'
  | 'switched-off'
  | 'failed';

const OBSERVED: Record<ResourceInstance['status']['state'], Observed> = {
  ready: 'running',
  degraded: 'degraded',
  pending: 'starting',
  starting: 'starting',
  disabled: 'stopped',
  stopping: 'stopped',
  stopped: 'stopped',
  orphaned: 'stopped',
  retrying: 'retrying',
  failed: 'failed',
};

// A module the host switched off keeps whatever state its last attempt left.
export const observedOf = (instance: ResourceInstance): Observed =>
  instance.status.memory?.switched_off === undefined
    ? OBSERVED[instance.status.state]
    : 'switched-off';

/** The words beside a row's switch: what the module does now, and why the switch is locked. */
export interface SwitchNotes {
  readonly observed?: string | undefined;
  readonly locked?: string | undefined;
}

export const switchNotesOf = (
  row: ExtensionRow,
  state: SwitchState,
  t: Translate,
): SwitchNotes => {
  const observed =
    row.instance === undefined
      ? undefined
      : t(`app.settings.extensions.observed.${observedOf(row.instance)}`);
  if (state.kind !== 'locked') return { observed };
  const locked =
    state.reason === 'required'
      ? t('app.settings.extensions.switch.locked.required', {
          names: formatList(state.names, 'conjunction'),
          count: state.names.length,
        })
      : t(`app.settings.extensions.switch.locked.${state.reason}`);

  return { observed, locked };
};

// A condition message is `<code>: <sentence>` when a fault raised it, such as
// `core.internal/RESOURCE_RECORDS_UNSERVED: the module keeps its data in …`.
const FAULT_MESSAGE = /^([a-z][a-z0-9.-]*\/[A-Z][A-Z0-9_]*): ([\s\S]+)$/;

/**
 * Why a module is not running, for every state but running, and nothing for a
 * stopped module with no stop recorded. A switch-off comes first, as the
 * host's own decision. Otherwise a missing dependency comes first, because it
 * names what the operator has to add and an older stop would hide it; then the
 * last stop, then any other condition that holds the module back, rendered by
 * its code.
 */
export const whyOf = (
  instance: ResourceInstance,
  t: Translate,
): FaultText | undefined => {
  const observed = observedOf(instance);
  if (observed === 'running') return undefined;

  const memory = instance.status.memory;
  const stop =
    memory?.switched_off === undefined
      ? memory?.last_stop
      : { at: memory.switched_off.at, result: 'switched-off-by-host' };
  const stopFault = (): FaultText | undefined =>
    stop === undefined
      ? undefined
      : {
          code: undefined,
          title: stopText(stop, t, memory),
          detail: undefined,
        };
  if (observed === 'switched-off') return stopFault();

  const held = (instance.status.conditions ?? []).filter(
    (condition) =>
      condition.status === 'False' && (condition.message ?? '') !== '',
  );
  const dependency = held.find(
    (condition) => condition.type === 'DependenciesSatisfied',
  );
  const faultOf = (condition: (typeof held)[number]): FaultText => {
    const message = condition.message ?? '';
    const coded = FAULT_MESSAGE.exec(message);

    return describeFault(
      coded === null ? { message } : { code: coded[1], message: coded[2] },
    );
  };
  if (observed === 'stopped') return stopFault();
  if (dependency !== undefined) return faultOf(dependency);
  if (observed !== 'degraded' && observed !== 'starting') {
    const fromStop = stopFault();
    if (fromStop !== undefined) return fromStop;
  }

  return held[0] === undefined ? undefined : faultOf(held[0]);
};
