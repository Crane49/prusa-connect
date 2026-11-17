import { describe, expect, it } from 'vitest';

import { type DeviceSelector, matchesDevice } from './device-selector';
import type { SlotActionsProps } from './slot-actions/types';
import type { SlotOutletProps } from './slot-outlet/types';

describe('the devices selector', () => {
  it('draws an entry that carries none for every device', () => {
    expect(matchesDevice(undefined, { resident: true })).toBe(true);
    expect(matchesDevice(undefined, {})).toBe(true);
    expect(matchesDevice(undefined, undefined)).toBe(true);
  });

  it('draws an entry that selects resident devices only for one', () => {
    const resident = { resident: true };
    expect(matchesDevice(resident, { resident: true })).toBe(true);
    expect(matchesDevice(resident, { resident: false })).toBe(false);
    expect(matchesDevice(resident, {})).toBe(false);
  });

  // ADR-20261110-5np rule 10: a condition this version cannot read is not
  // met, so a newer server's selector never shows the entry everywhere. The
  // listing is read as JSON, and nothing checks the selector's keys.
  it('draws no entry whose selector names a condition it cannot read', () => {
    const fromWire = (json: string): DeviceSelector =>
      JSON.parse(json) as DeviceSelector;
    const resident = { resident: true };
    expect(matchesDevice(fromWire('{"kind":"camera"}'), resident)).toBe(false);
    expect(
      matchesDevice(fromWire('{"resident":true,"kind":"camera"}'), resident),
    ).toBe(false);
    expect(matchesDevice(fromWire('{"resident":false}'), resident)).toBe(false);
    expect(matchesDevice(fromWire('{"resident":false}'), {})).toBe(false);
  });

  it('reads a key named like a property of every object as unknown', () => {
    const resident = { resident: true };
    for (const key of ['__proto__', 'valueOf', 'hasOwnProperty', 'toString']) {
      const selector = JSON.parse(`{"${key}":true}`) as DeviceSelector;
      expect(matchesDevice(selector, resident)).toBe(false);
    }
  });

  it('skips a condition a caller left undefined', () => {
    const selector = { resident: true, kind: undefined } as DeviceSelector;
    expect(matchesDevice(selector, { resident: true })).toBe(true);
  });

  // ADR-20261110-5np rule 10: the server never sends a block that names no
  // condition, so one that arrives lost a condition on the way.
  it('draws no entry whose selector names no condition', () => {
    const resident = { resident: true };
    for (const json of ['{}', 'null', '[]', 'true', '0', '"resident"']) {
      const selector = JSON.parse(json) as DeviceSelector;
      expect(matchesDevice(selector, resident), json).toBe(false);
    }
    expect(matchesDevice({ resident: undefined }, resident)).toBe(false);
  });

  it('draws no such entry while the device has no record', () => {
    expect(matchesDevice({ resident: true }, undefined)).toBe(false);
  });

  it('is a prop of a slot handed a device, and of no other slot', () => {
    const tab: SlotOutletProps = {
      screen: 'printer-detail',
      name: 'device-tab',
      deviceId: 'a',
      device: { resident: true },
    };
    const menu: SlotActionsProps = {
      name: 'device-action',
      deviceId: 'a',
      label: 'A',
      device: {},
    };
    const file: SlotOutletProps = {
      screen: 'storage',
      name: 'file-tab',
      fileRef: 'local:a.gcode',
      // @ts-expect-error -- a slot handed a file takes no device record
      device: {},
    };
    const fileMenu: SlotActionsProps = {
      name: 'file-action',
      fileRef: 'local:a.gcode',
      label: 'a.gcode',
      // @ts-expect-error -- nor does the menu of a file
      device: {},
    };

    expect([tab, menu, file, fileMenu].map((props) => props.name)).toEqual([
      'device-tab',
      'device-action',
      'file-tab',
      'file-action',
    ]);
  });
});
