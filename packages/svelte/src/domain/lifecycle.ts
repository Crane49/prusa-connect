// Moved from pull request 185, packages/domain/src/lifecycle.ts, 2026-09-11
import {
  type CommandProjection,
  type Device,
  projectCommand,
  type StateTransition,
} from './command';

export const LIFECYCLE_VERBS = [
  'pause_print',
  'resume_print',
  'stop_print',
  'start_print',
] as const;

export type LifecycleVerb = (typeof LIFECYCLE_VERBS)[number];

/** The capability key the core publishes for a verb, as `capabilities` lists it. */
export const capabilityFor = (verb: LifecycleVerb): string =>
  `core.command.${verb}`;

/**
 * The four lifecycle moves, in the shape the device-state vocabulary
 * publishes, so `projectCommand` reads a static list and a fetched one by
 * one rule. A page that has the vocabulary passes that instead.
 */
export const LIFECYCLE_TRANSITIONS: readonly StateTransition[] = [
  { from: 'PRINTING', to: 'PAUSED', verb: capabilityFor('pause_print') },
  { from: 'PAUSED', to: 'PRINTING', verb: capabilityFor('resume_print') },
  { from: 'PRINTING', to: 'STOPPED', verb: capabilityFor('stop_print') },
  { from: 'PAUSED', to: 'STOPPED', verb: capabilityFor('stop_print') },
  { from: 'IDLE', to: 'PRINTING', verb: capabilityFor('start_print') },
  { from: 'READY', to: 'PRINTING', verb: capabilityFor('start_print') },
  { from: 'FINISHED', to: 'PRINTING', verb: capabilityFor('start_print') },
  { from: 'STOPPED', to: 'PRINTING', verb: capabilityFor('start_print') },
];

export const projectLifecycleVerb = (
  verb: LifecycleVerb,
  {
    capabilities,
    state,
    locked = false,
  }: {
    /** What the printer reported it can do, verbatim from the device record. */
    capabilities: Device['capabilities'];
    /** Its state now, or `undefined` while it has reported none. */
    state: string | undefined;
    /** Held by another operator or a running command. Outranks state. */
    locked?: boolean;
  },
): CommandProjection =>
  projectCommand(capabilityFor(verb), {
    capabilities,
    transitions: LIFECYCLE_TRANSITIONS,
    state,
    locked,
  });
