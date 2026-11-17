// Moved from pull request 185, packages/domain/src/command.ts, 2026-09-11
import type { components } from '@prusa/connect';

/** The device record `GET /api/v1/devices/{device_id}` answers. */
export type Device = components['schemas']['Device'];

/**
 * One move between two states and the verb that causes it, as
 * `GET /api/v1/vocabularies/device-states` publishes it for a device kind.
 */
export type StateTransition = components['schemas']['DeviceStateTransition'];

/**
 * Why a verb is refused, as a catalogue key and the states that go into the
 * message. It is a key and not a sentence because this layer has no language:
 * a screen turns it into words through `refusalText`, in whichever language
 * the person reads (frontend-i18n.md rule 1).
 */
export interface CommandRefusal {
  /** The catalogue key: `command.refused.locked` and its siblings. */
  key: string;
  /** The states the verb runs from, as the adapter spells them on the wire. */
  states?: readonly string[];
}

/**
 * `absent` when the printer never offered the verb, `disabled` with the
 * reason, `available` otherwise. A caller that draws only the available ones
 * is free to; the reason exists for the surfaces that list every command.
 */
export type CommandProjection =
  | { kind: 'absent' }
  | { kind: 'disabled'; reason: CommandRefusal }
  | { kind: 'available' };

export interface CommandContext {
  /** The verbs the printer accepts, verbatim from the device record. `null` is what the API answers for none. */
  capabilities: Device['capabilities'];
  /** The moves the device kind declared. A verb no transition names is taken as executable from any state. */
  transitions?: readonly StateTransition[];
  /** The state the printer last reported, or `undefined` while it has reported none. */
  state: string | undefined;
  /** Held by another operator or a running command. Outranks state. */
  locked?: boolean;
  /**
   * The verbs the caller may send, verbatim from the device record. A verb the
   * printer accepts and the caller may not send is refused before anything
   * else, because no state makes it sendable. `null` and `undefined` refuse
   * nothing: a record that does not say is left to the server, which refuses
   * it anyway.
   */
  permitted?: Device['verbs'];
}

export const projectCommand = (
  verb: string,
  {
    capabilities,
    transitions = [],
    state,
    locked = false,
    permitted,
  }: CommandContext,
): CommandProjection => {
  if (!(capabilities ?? []).includes(verb)) return { kind: 'absent' };

  if (permitted != null && !permitted.includes(verb)) {
    return {
      kind: 'disabled',
      reason: { key: 'command.refused.notPermitted' },
    };
  }

  if (locked) {
    return { kind: 'disabled', reason: { key: 'command.refused.locked' } };
  }

  // A printer that has reported no state yet is not in a wrong state. It is
  // unknown, and refusing with a list of allowed states would name states it
  // may well be in.
  if (state === undefined) {
    return {
      kind: 'disabled',
      reason: { key: 'command.refused.notReported' },
    };
  }

  const allowedFrom = [
    ...new Set(
      transitions
        .filter((transition) => transition.verb === verb)
        .map((transition) => transition.from),
    ),
  ];
  if (allowedFrom.length > 0 && !allowedFrom.includes(state)) {
    return {
      kind: 'disabled',
      reason: { key: 'command.refused.states', states: allowedFrom },
    };
  }

  return { kind: 'available' };
};
