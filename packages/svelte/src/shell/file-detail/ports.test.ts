import { describe, expect, it } from 'vitest';

import type { DeviceEntry, StorageEntry } from '../../data';
import type { FileDetailEntry } from './types';

const onStorage: StorageEntry = {
  folder: false,
  modified: '2026-08-18T14:02:00Z',
  name: 'Bracket_Left_Reinforced_v4_FINAL.gcode',
  path: 'Bracket_Left_Reinforced_v4_FINAL.gcode',
  sha256: '9f2c1ab4d5e6f70819a2b3c4d5e6f70819a2b3c4d5e6f70819a2b3c4d5e6f708',
  size: 19_293_798,
};

const onPrinter: DeviceEntry = {
  folder: false,
  modified: '2026-08-18T12:20:00Z',
  name: 'RETRAC~1.BGC',
  display_name: 'retract_tune.bgcode',
  path: 'usb/calibration/retract_tune.bgcode',
  read_only: true,
  size: 1_153_434,
};

describe('the file port', () => {
  // Structural typing makes the match silent in both directions, so it is
  // asserted rather than assumed. The two records are the reason this is one
  // screen and not two: neither carries an id, and what they share is enough
  // to draw the page.
  it('takes a file this server keeps', () => {
    const entry: FileDetailEntry = onStorage;

    expect(entry.sha256).toHaveLength(64);
    expect(entry.read_only).toBeUndefined();
  });

  it('takes a file the printer keeps', () => {
    const entry: FileDetailEntry = onPrinter;

    expect(entry.display_name).toBe('retract_tune.bgcode');
    expect(entry.sha256).toBeUndefined();
  });
});
