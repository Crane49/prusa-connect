// Moved from pull request 185, packages/domain/src/command.test.ts, 2026-09-11
import { describe, expect, it } from 'vitest';

import { projectCommand, type StateTransition } from './command';

const SET_SPEED = 'core.command.set_speed';
const PAUSE = 'core.command.pause_print';

// The moves a device kind declares, in the shape the vocabulary publishes.
const transitions: StateTransition[] = [
  { from: 'PRINTING', to: 'PAUSED', verb: PAUSE },
  { from: 'PRINTING', to: 'PRINTING', verb: SET_SPEED },
  { from: 'PAUSED', to: 'PAUSED', verb: SET_SPEED },
  // A move the device makes alone names no verb and gates nothing.
  { from: 'PRINTING', to: 'FINISHED' },
];

describe('projectCommand', () => {
  it('reports a command the printer never offered as absent', () => {
    expect(
      projectCommand(PAUSE, { capabilities: [], state: 'PRINTING' }),
    ).toEqual({ kind: 'absent' });
  });

  it('reads the null capability set the API answers for none as none', () => {
    expect(
      projectCommand(PAUSE, { capabilities: null, state: 'PRINTING' }),
    ).toEqual({ kind: 'absent' });
  });

  it('names the states a command needs when the printer is in none of them', () => {
    expect(
      projectCommand(SET_SPEED, {
        capabilities: [SET_SPEED],
        transitions,
        state: 'IDLE',
      }),
    ).toEqual({
      kind: 'disabled',
      reason: {
        key: 'command.refused.states',
        states: ['PRINTING', 'PAUSED'],
      },
    });
  });

  it('joins three or more states with commas and a final or', () => {
    expect(
      projectCommand(PAUSE, {
        capabilities: [PAUSE],
        transitions: [
          { from: 'PRINTING', to: 'PAUSED', verb: PAUSE },
          { from: 'BUSY', to: 'PAUSED', verb: PAUSE },
          { from: 'ATTENTION', to: 'PAUSED', verb: PAUSE },
        ],
        state: 'IDLE',
      }),
    ).toEqual({
      kind: 'disabled',
      reason: {
        key: 'command.refused.states',
        states: ['PRINTING', 'BUSY', 'ATTENTION'],
      },
    });
  });

  it('reads a command with no state list as executable from any state', () => {
    expect(
      projectCommand('core.command.stop_print', {
        capabilities: ['core.command.stop_print'],
        transitions,
        state: 'OFFLINE',
      }),
    ).toEqual({ kind: 'available' });
  });

  it('disables every command on a locked printer, whatever its state', () => {
    // A lock outranks state: a printer someone else holds cannot take a
    // command that its state would otherwise allow.
    expect(
      projectCommand(SET_SPEED, {
        capabilities: [SET_SPEED],
        transitions,
        state: 'PRINTING',
        locked: true,
      }),
    ).toEqual({ kind: 'disabled', reason: { key: 'command.refused.locked' } });
  });

  // A dangerous verb needs ownership or a grant, which no state gives, so the
  // refusal names the caller and not the printer.
  it('disables a command the caller may not send, whatever its state', () => {
    expect(
      projectCommand(SET_SPEED, {
        capabilities: [SET_SPEED],
        transitions,
        state: 'PRINTING',
        locked: true,
        permitted: [PAUSE],
      }),
    ).toEqual({
      kind: 'disabled',
      reason: { key: 'command.refused.notPermitted' },
    });
  });

  it('refuses nothing for a record that did not say what the caller may send', () => {
    expect(
      projectCommand(SET_SPEED, {
        capabilities: [SET_SPEED],
        transitions,
        state: 'PRINTING',
        permitted: null,
      }),
    ).toEqual({ kind: 'available' });
  });

  it('prefers absence to a lock, so a missing capability is never blamed on the lock', () => {
    expect(
      projectCommand(SET_SPEED, {
        capabilities: [],
        transitions,
        state: 'PRINTING',
        locked: true,
      }),
    ).toEqual({ kind: 'absent' });
  });

  it('does not name allowed states for a printer that has reported none', () => {
    expect(
      projectCommand(SET_SPEED, {
        capabilities: [SET_SPEED],
        transitions,
        state: undefined,
      }),
    ).toEqual({
      kind: 'disabled',
      reason: { key: 'command.refused.notReported' },
    });
  });

  it('spells a state it has no label for the way the adapter did', () => {
    expect(
      projectCommand(PAUSE, {
        capabilities: [PAUSE],
        transitions: [{ from: 'FIN_OK', to: 'PAUSED', verb: PAUSE }],
        state: 'IDLE',
      }),
    ).toEqual({
      kind: 'disabled',
      reason: { key: 'command.refused.states', states: ['FIN_OK'] },
    });
  });
});
