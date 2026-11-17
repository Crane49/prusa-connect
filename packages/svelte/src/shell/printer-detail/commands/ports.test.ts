import { describe, expect, it } from 'vitest';

import { DeviceCommands } from '../../../data';
import type { DeviceCommandsClient } from '../../../data';
import type { CommandsSource } from './types';

const refuse = () => {
  throw new Error('this fake reaches no server');
};

const client: DeviceCommandsClient = {
  sendDeviceCommand: refuse,
  sendPrinterGcode: refuse,
  listGcodePresets: refuse,
  runPrinterGcodePreset: refuse,
  getCommand: refuse,
};

describe('the commands port', () => {
  it('is satisfied by the store this product hands in', () => {
    // Structural typing makes the match silent in both directions, so it is
    // asserted rather than assumed.
    const source: CommandsSource = new DeviceCommands(client, 'printer-1');

    expect(source.log).toEqual([]);
    expect(source.presets).toEqual([]);
    expect(source.sending).toBe(false);
    expect(source.refused).toBe(0);
  });
});
