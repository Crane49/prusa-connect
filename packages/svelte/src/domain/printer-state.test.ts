// Moved from pull request 185, packages/domain/src/printer-state.test.ts, 2026-09-11
import { describe, expect, it } from 'vitest';

import {
  isPrinterState,
  PRINTER_STATE_LABEL_KEY,
  PRINTER_STATE_PROGRESS_TONE,
  PRINTER_STATE_SEVERITY,
  PRINTER_STATE_SEVERITY_TIER,
  PRINTER_STATE_TONE,
  PRINTER_STATES,
  type PrinterSeverityTier,
  printerStateOf,
} from './printer-state';

describe('printer state', () => {
  it('covers every state the core connector writes', () => {
    expect(PRINTER_STATES).toContain('MANIPULATING');
    expect(PRINTER_STATES).toHaveLength(12);
  });

  it('assigns a severity to every state, with error the most severe', () => {
    for (const state of PRINTER_STATES) {
      expect(PRINTER_STATE_SEVERITY[state]).toBeTypeOf('number');
    }
    const worst = [...PRINTER_STATES].sort(
      (a, b) => PRINTER_STATE_SEVERITY[a] - PRINTER_STATE_SEVERITY[b],
    )[0];
    expect(worst).toBe('ERROR');
  });
});

describe('printerStateOf', () => {
  it('keeps a word this library has rules for', () => {
    expect(printerStateOf('PRINTING')).toBe('PRINTING');
    expect(isPrinterState('PAUSED')).toBe(true);
  });

  // The SDK types the state as an open string: an adapter declares its own
  // set, and a server may install one this library has never seen. Such a
  // word lands in the quiet tier rather than throwing on a missing key.
  it('folds a word an adapter declared and this library does not know to UNKNOWN', () => {
    expect(printerStateOf('FIN_OK')).toBe('UNKNOWN');
    expect(isPrinterState('printing')).toBe(false);
    expect(PRINTER_STATE_SEVERITY_TIER[printerStateOf('FIN_OK')]).toBe(
      'not-reporting',
    );
  });
});

describe('state policy maps', () => {
  // A Record over PrinterState is the point: a thirteenth state must break the
  // build at the declaration rather than at whichever call site first reads a
  // missing key. The test checks the same property at runtime, because a
  // `Record` typed from a widened union would satisfy the compiler while
  // leaving a hole.
  it('covers every printer state', () => {
    for (const state of PRINTER_STATES) {
      expect(PRINTER_STATE_TONE[state]).toBeDefined();
      expect(PRINTER_STATE_PROGRESS_TONE[state]).toBeDefined();
      expect(PRINTER_STATE_SEVERITY_TIER[state]).toBeDefined();
    }
  });

  // The tier map and the numeric severity rank are two views of one
  // ordering, and they must not drift. Pinned rank per tier rather than a
  // relative check, so moving a state between tiers without moving its rank
  // fails here rather than shipping a fleet sorted one way and filtered
  // another.
  const TIER_RANK: Record<PrinterSeverityTier, number> = {
    critical: 0,
    attention: 1,
    'not-reporting': 2,
    normal: 3,
  };

  it('agrees with the numeric severity ranking', () => {
    for (const state of PRINTER_STATES) {
      const tier = PRINTER_STATE_SEVERITY_TIER[state];
      const rank = PRINTER_STATE_SEVERITY[state];
      expect(rank).toBeGreaterThanOrEqual(TIER_RANK[tier]);
      // Only `normal` spans more than one rank: it holds the printers doing
      // work, the ones done and the ones sitting idle, and the fleet sort
      // keeps those apart while the filter strip treats them as one.
      if (tier !== 'normal') expect(rank).toBe(TIER_RANK[tier]);
    }
  });

  it('ranks a paused print above a printer merely running', () => {
    expect(PRINTER_STATE_SEVERITY.PAUSED).toBeLessThan(
      PRINTER_STATE_SEVERITY.PRINTING,
    );
  });

  it('ranks a done printer between the working and the idle ones', () => {
    for (const state of ['FINISHED', 'STOPPED'] as const) {
      expect(PRINTER_STATE_SEVERITY[state]).toBeGreaterThan(
        PRINTER_STATE_SEVERITY.PRINTING,
      );
      expect(PRINTER_STATE_SEVERITY[state]).toBeLessThan(
        PRINTER_STATE_SEVERITY.IDLE,
      );
    }
  });
});

describe('PRINTER_STATE_LABEL_KEY', () => {
  // The defect this map exists to remove is a wire value on screen, so the
  // test worth having is the one that catches a key that is really the enum.
  it('names a catalogue key for every state', () => {
    for (const state of PRINTER_STATES) {
      const key = PRINTER_STATE_LABEL_KEY[state];
      expect(key).toBe(`state.${state}`);
    }
  });
});
