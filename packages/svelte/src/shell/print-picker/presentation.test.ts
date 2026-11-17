import { describe, expect, it } from 'vitest';

import { START_PRINT_VERB } from '../../data/services/print';
import { STATE_PATH } from '../../domain';
import { fakeDevice } from '../../test/fakes';
import type { Device, StateTransition } from '../../domain';
import {
  FREE_STATES,
  printPhaseOf,
  printTargetOf,
  printTargetsOf,
  REASON_KEY,
  slicedForOther,
  STATE_REASON_KEY,
} from './presentation';

const printerOf = (
  id: string,
  state: string,
  over: Partial<Device> = {},
): Device => ({
  ...fakeDevice(id, id),
  capabilities: [START_PRINT_VERB],
  state: { [STATE_PATH]: state },
  ...over,
});

/** What a kind that publishes its own moves declares for the start verb. */
const ECHO: StateTransition[] = [
  { from: 'IDLE', to: 'PRINTING', verb: START_PRINT_VERB },
];

describe('printTargetOf', () => {
  it('offers a printer whose bed is free', () => {
    for (const state of FREE_STATES) {
      const target = printTargetOf(printerOf('one', state));

      expect([state, target.ready]).toEqual([state, true]);
      expect(target.reasonKey).toBeUndefined();
    }
  });

  // The part of the last print is still on the bed until somebody takes it
  // off, and the firmware takes a start command in both states, so a picker
  // that offered them would print into that part.
  it('refuses a printer with a finished or a stopped print on its bed', () => {
    const finished = printTargetOf(printerOf('one', 'FINISHED'));
    expect(finished.ready).toBe(false);
    expect(finished.reasonKey).toBe(STATE_REASON_KEY.FINISHED);

    const stopped = printTargetOf(printerOf('one', 'STOPPED'));
    expect(stopped.ready).toBe(false);
    expect(stopped.reasonKey).toBe(STATE_REASON_KEY.STOPPED);
  });

  // One sentence per state, so a reader is told what the machine is doing and
  // not only that it is unavailable.
  it('names what each blocked printer is doing', () => {
    const said: Record<string, string | undefined> = {};
    for (const state of [
      'ATTENTION',
      'BUSY',
      'ERROR',
      'MANIPULATING',
      'PAUSED',
      'PRINTING',
    ]) {
      said[state] = printTargetOf(printerOf('one', state)).reasonKey;
    }

    expect(said).toEqual({
      ATTENTION: STATE_REASON_KEY.ATTENTION,
      BUSY: STATE_REASON_KEY.BUSY,
      ERROR: STATE_REASON_KEY.ERROR,
      MANIPULATING: STATE_REASON_KEY.MANIPULATING,
      PAUSED: STATE_REASON_KEY.PAUSED,
      PRINTING: STATE_REASON_KEY.PRINTING,
    });
    expect(new Set(Object.values(said)).size).toBe(6);
  });

  it('refuses a printer this server cannot hear from', () => {
    const target = printTargetOf(
      printerOf('one', 'IDLE', { connected: false }),
    );

    expect(target.ready).toBe(false);
    expect(target.reasonKey).toBe(REASON_KEY.offline);
    expect(target.state).toBe('OFFLINE');
  });

  it('refuses a printer that does not take the print command', () => {
    const target = printTargetOf(
      printerOf('one', 'IDLE', { capabilities: [] }),
    );

    expect(target.ready).toBe(false);
    expect(target.reasonKey).toBe(REASON_KEY.noVerb);
  });

  it('refuses a printer that has reported no state', () => {
    const target = printTargetOf(printerOf('one', 'IDLE', { state: {} }));

    expect(target.ready).toBe(false);
    expect(target.reasonKey).toBe('command.refused.notReported');
  });

  it('refuses a printer another command is holding', () => {
    const held = printTargetOf(printerOf('one', 'IDLE'), { locked: ['one'] });
    expect(held.ready).toBe(false);
    expect(held.reasonKey).toBe(REASON_KEY.locked);

    const free = printTargetOf(printerOf('one', 'IDLE'), { locked: ['two'] });
    expect(free.ready).toBe(true);
  });

  // A file from this server has nowhere to land on a printer with no USB drive.
  it('refuses a printer that answered with no drive to write to', () => {
    const bare = printTargetOf(printerOf('one', 'IDLE'), {
      noStorage: ['one'],
    });
    expect(bare.ready).toBe(false);
    expect(bare.reasonKey).toBe(REASON_KEY.noStorage);

    const offline = printTargetOf(
      { ...printerOf('one', 'IDLE'), connected: false },
      { noStorage: ['one'] },
    );
    expect(offline.reasonKey).toBe(REASON_KEY.offline);
  });

  it('refuses a printer whose word this library has no rule for', () => {
    const target = printTargetOf(printerOf('one', 'DEFROSTING'));

    expect(target.state).toBe('UNKNOWN');
    expect(target.ready).toBe(false);
    expect(target.reasonKey).toBe(STATE_REASON_KEY.UNKNOWN);
  });

  // A kind that publishes the states its start verb runs from is the authority
  // on its own machine, and the IDLE and READY table is only the answer for a
  // kind that publishes none.
  it('follows the states the device kind declared for the start verb', () => {
    const ready = printTargetOf(printerOf('one', 'READY'), {
      transitions: ECHO,
    });
    expect(ready.ready).toBe(false);
    expect(ready.reasonKey).toBe('command.refused.states');
    expect(ready.reasonStates).toEqual(['IDLE']);

    const idle = printTargetOf(printerOf('one', 'IDLE'), { transitions: ECHO });
    expect(idle.ready).toBe(true);
  });

  // A kind that declares moves for other verbs and none for the start verb has
  // said nothing about starting a print, so the table decides.
  it('falls back to the free bed when the kind declares no start', () => {
    const other: StateTransition[] = [
      { from: 'PRINTING', to: 'PAUSED', verb: 'core.command.pause_print' },
    ];

    expect(
      printTargetOf(printerOf('one', 'READY'), { transitions: other }).ready,
    ).toBe(true);
    expect(
      printTargetOf(printerOf('one', 'FINISHED'), { transitions: other }).ready,
    ).toBe(false);
  });
});

describe('printTargetsOf', () => {
  it('puts the printers that can take the file first, then orders by name', () => {
    const rows = printTargetsOf([
      printerOf('zulu', 'IDLE'),
      printerOf('alpha', 'PRINTING'),
      printerOf('mike', 'IDLE'),
      printerOf('bravo', 'ERROR'),
    ]);

    expect(rows.map((row) => row.id)).toEqual([
      'mike',
      'zulu',
      'alpha',
      'bravo',
    ]);
  });

  it('keeps every printer, so a reader finds the one they came for', () => {
    const rows = printTargetsOf([
      printerOf('one', 'PRINTING'),
      printerOf('two', 'ERROR'),
    ]);

    expect(rows).toHaveLength(2);
    expect(rows.every((row) => !row.ready)).toBe(true);
  });

  it('hands the moves the kind declared down to every row', () => {
    const rows = printTargetsOf([printerOf('one', 'READY')], {
      transitions: ECHO,
    });

    expect(rows[0]?.reasonKey).toBe('command.refused.states');
  });
});

describe('slicedForOther', () => {
  it('warns about a printer of another model than the file names', () => {
    expect(slicedForOther({ model: 'XL' }, 'MK4S')).toBe(true);
  });

  it('meets the slicer spelling of the same model', () => {
    expect(slicedForOther({ model: 'CoreOne' }, 'COREONE')).toBe(false);
    expect(slicedForOther({ model: 'MK4S' }, 'mk4s')).toBe(false);
  });

  it('says nothing when either side named no model', () => {
    expect(slicedForOther({ model: 'MK4S' }, undefined)).toBe(false);
    expect(slicedForOther({ model: 'MK4S' }, '')).toBe(false);
    expect(slicedForOther({ model: '' }, 'MK4S')).toBe(false);
  });
});

describe('printPhaseOf', () => {
  it('reads nothing while no transfer is running', () => {
    expect(printPhaseOf(undefined)).toBeUndefined();
  });

  it('reads how far the bytes got while they move', () => {
    expect(printPhaseOf({ bytesSent: 750, bytesTotal: 3000 })).toEqual({
      step: 'sending',
      progress: 0.25,
    });
    expect(printPhaseOf({ bytesSent: 0, bytesTotal: 0 })).toEqual({
      step: 'sending',
      progress: null,
    });
  });

  // An MK4S takes most of a minute between the last byte and the transfer
  // finishing, which a bar frozen at 100% would leave unexplained.
  it('reads the printer writing once every byte is out', () => {
    expect(printPhaseOf({ bytesSent: 3000, bytesTotal: 3000 })).toEqual({
      step: 'writing',
    });
  });
});
