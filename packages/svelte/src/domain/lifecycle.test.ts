// Moved from pull request 185, packages/domain/src/lifecycle.test.ts, 2026-09-11
import { describe, expect, it } from 'vitest';

import {
  capabilityFor,
  LIFECYCLE_TRANSITIONS,
  LIFECYCLE_VERBS,
  projectLifecycleVerb,
} from './lifecycle';

const withAll = LIFECYCLE_VERBS.map(capabilityFor);

describe('projectLifecycleVerb', () => {
  it.each([
    ['pause_print', 'PRINTING', 'available'],
    ['pause_print', 'PAUSED', 'disabled'],
    ['pause_print', 'IDLE', 'disabled'],
    ['resume_print', 'PAUSED', 'available'],
    ['resume_print', 'PRINTING', 'disabled'],
    ['stop_print', 'PRINTING', 'available'],
    ['stop_print', 'PAUSED', 'available'],
    ['stop_print', 'FINISHED', 'disabled'],
    ['start_print', 'IDLE', 'available'],
    ['start_print', 'FINISHED', 'available'],
    ['start_print', 'PRINTING', 'disabled'],
  ] as const)('%s from %s is %s', (verb, state, kind) => {
    expect(
      projectLifecycleVerb(verb, { capabilities: withAll, state }).kind,
    ).toBe(kind);
  });

  // The distinction the whole module exists for: a printer that cannot do a
  // thing is not a printer refusing to do it now.
  it('reports a capability the printer never declared as absent, with no reason', () => {
    const projection = projectLifecycleVerb('pause_print', {
      capabilities: [],
      state: 'PRINTING',
    });
    expect(projection).toEqual({ kind: 'absent' });
  });

  it('prefers absent over a state refusal, so a missing capability is never explained away', () => {
    expect(
      projectLifecycleVerb('pause_print', { capabilities: [], state: 'IDLE' }),
    ).toEqual({ kind: 'absent' });
  });

  it('lets a lock outrank a state that would otherwise allow the command', () => {
    const projection = projectLifecycleVerb('pause_print', {
      capabilities: withAll,
      state: 'PRINTING',
      locked: true,
    });
    expect(projection).toEqual({
      kind: 'disabled',
      reason: { key: 'command.refused.locked' },
    });
  });

  it('does not name allowed states for a printer that has reported none', () => {
    const projection = projectLifecycleVerb('pause_print', {
      capabilities: withAll,
      state: undefined,
    });
    expect(projection).toEqual({
      kind: 'disabled',
      reason: { key: 'command.refused.notReported' },
    });
  });

  // ATTENTION and ERROR are the two states a machine is in when it is asking
  // for something. Offering Start there buries the reason it stopped.
  it.each(['ATTENTION', 'ERROR'] as const)(
    'refuses start_print from %s',
    (state) => {
      expect(
        projectLifecycleVerb('start_print', {
          capabilities: withAll,
          state,
        }).kind,
      ).toBe('disabled');
    },
  );

  it('names the states that would allow the command', () => {
    const projection = projectLifecycleVerb('resume_print', {
      capabilities: withAll,
      state: 'PRINTING',
    });
    expect(projection).toEqual({
      kind: 'disabled',
      reason: { key: 'command.refused.states', states: ['PAUSED'] },
    });
  });

  it('reads a capability key the way the core publishes it', () => {
    expect(capabilityFor('pause_print')).toBe('core.command.pause_print');
  });

  it('covers the four verbs of a printer lifecycle', () => {
    expect([...LIFECYCLE_VERBS]).toEqual([
      'pause_print',
      'resume_print',
      'stop_print',
      'start_print',
    ]);
    for (const verb of LIFECYCLE_VERBS) {
      expect(
        LIFECYCLE_TRANSITIONS.some(
          (transition) => transition.verb === capabilityFor(verb),
        ),
      ).toBe(true);
    }
  });
});
