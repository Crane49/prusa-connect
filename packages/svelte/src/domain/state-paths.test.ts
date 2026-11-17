import { describe, expect, it } from 'vitest';

import { fakeDevice } from '../test/fakes';
import {
  DIALOG_BUTTONS_PATH,
  DIALOG_CODE_PATH,
  DIALOG_ID_PATH,
  DIALOG_TEXT_PATH,
  DIALOG_TITLE_PATH,
  dialogAnswer,
  dialogButtonLabel,
  dialogOf,
  NOZZLE_PATH,
  numberAt,
  activeHeadOf,
  activeMaterialOf,
  toolArgument,
  toolsOf,
  STATE_PATH,
  textAt,
} from './state-paths';

describe('dialogOf', () => {
  it('answers nothing while the printer waits on no dialog', () => {
    const device = fakeDevice('p1');
    device.state = { [DIALOG_TITLE_PATH]: 'Left behind' };

    expect(dialogOf(device)).toBeUndefined();
  });

  it('reads the question and keeps only the labels that are words', () => {
    const device = fakeDevice('p1');
    device.state = {
      [DIALOG_ID_PATH]: 7,
      [DIALOG_TITLE_PATH]: 'Filament runout',
      [DIALOG_TEXT_PATH]: 'Insert filament, then press Retry.',
      [DIALOG_BUTTONS_PATH]: ['Retry', '', 3, 'Cancel'],
    };

    expect(dialogOf(device)).toEqual({
      id: 7,
      code: undefined,
      title: 'Filament runout',
      text: 'Insert filament, then press Retry.',
      buttons: ['Retry', 'Cancel'],
    });
  });

  // A filament change arrives this way: the buttons and the firmware's code
  // for the question, and no words at all. The code is the only thing that
  // says which question a reader is being asked to answer, and it arrives as
  // the printer encoded it: a word, zero-padded in codes such as `04502`.
  it('reads the code of a question the printer sent no words for', () => {
    const device = fakeDevice('p1');
    device.state = {
      [DIALOG_ID_PATH]: 7,
      [DIALOG_CODE_PATH]: '04502',
      [DIALOG_BUTTONS_PATH]: ['Yes', 'No', 'Purge_more'],
    };

    expect(dialogOf(device)).toEqual({
      id: 7,
      code: '04502',
      title: undefined,
      text: undefined,
      buttons: ['Yes', 'No', 'Purge_more'],
    });
  });

  it('answers a dialog with no buttons when the printer sent none', () => {
    const device = fakeDevice('p1');
    device.state = { [DIALOG_ID_PATH]: 7 };

    expect(dialogOf(device)?.buttons).toEqual([]);
  });

  // The verb names the dialog by id and the button by its label, as the
  // firmware wrote it.
  it('names the dialog and the pressed label in the reply', () => {
    const device = fakeDevice('p1');
    device.state = { [DIALOG_ID_PATH]: 7, [DIALOG_BUTTONS_PATH]: ['Retry'] };
    const dialog = dialogOf(device);
    if (dialog === undefined) throw new Error('expected a dialog');

    expect(dialogAnswer(dialog, 'Retry')).toEqual({
      dialog_id: '7',
      button: 'Retry',
    });
  });
});

describe('dialogButtonLabel', () => {
  // The underscores are the firmware's spelling and the whole name is what
  // travels back as the answer, so only the label loses them.
  it('reads the firmware spelling as words', () => {
    expect(dialogButtonLabel('Purge_more')).toBe('Purge more');
  });

  it('leaves a name with no underscore as it is', () => {
    expect(dialogButtonLabel('Yes')).toBe('Yes');
  });
});

describe('textAt', () => {
  it('answers the word the printer wrote', () => {
    const device = fakeDevice('p1');
    device.state = { [STATE_PATH]: 'PRINTING' };

    expect(textAt(device, STATE_PATH)).toBe('PRINTING');
  });

  it('answers nothing for a path the printer never wrote', () => {
    expect(textAt(fakeDevice('p1'), STATE_PATH)).toBeUndefined();
  });

  it('refuses a value of the wrong type rather than rendering it', () => {
    const device = fakeDevice('p1');
    device.state = { [STATE_PATH]: 42, [NOZZLE_PATH]: 'hot' };

    expect(textAt(device, STATE_PATH)).toBeUndefined();
    expect(numberAt(device, NOZZLE_PATH)).toBeUndefined();
  });

  it('reads an empty word as no reading, because it names no state', () => {
    const device = fakeDevice('p1');
    device.state = { [STATE_PATH]: '' };

    expect(textAt(device, STATE_PATH)).toBeUndefined();
  });
});

describe('numberAt', () => {
  it('keeps a reading of zero, which is not the same as no reading', () => {
    const device = fakeDevice('p1');
    device.state = { [NOZZLE_PATH]: 0 };

    expect(numberAt(device, NOZZLE_PATH)).toBe(0);
  });

  it('refuses a figure that is not a number', () => {
    const device = fakeDevice('p1');
    device.state = { [NOZZLE_PATH]: Number.NaN };

    expect(numberAt(device, NOZZLE_PATH)).toBeUndefined();
  });
});

describe('toolsOf', () => {
  const live = (values: Record<string, unknown>, paths?: string[]) => ({
    value: (path: string) => values[path],
    ...(paths === undefined ? {} : { paths }),
  });
  const MATERIAL = 'material_package_instance/package/material/abbreviation';

  it('orders heads by index from 0, not by spelling, and marks the active one', () => {
    const device = fakeDevice('xl');
    device.state = {
      '/tools/1/active': true,
      '/tools/10/nozzle_temp': 30,
      '/tools/1/nozzle_temp': 215,
      [`/tools/1/${MATERIAL}`]: 'PETG',
      [`/tools/0/${MATERIAL}`]: 'PLA',
    };

    expect(toolsOf(live({}), device)).toEqual([
      { index: 0, active: false, material: 'PLA', features: {} },
      {
        index: 1,
        active: true,
        temperature: 215,
        material: 'PETG',
        features: {},
      },
      { index: 10, active: false, temperature: 30, features: {} },
    ]);
  });

  it('prefers the live reading and finds heads only the stream has seen', () => {
    const device = fakeDevice('xl');
    device.state = { '/tools/0/nozzle_temp': 100, '/tools/0/active': true };

    expect(
      toolsOf(
        live(
          {
            '/tools/0/nozzle_temp': 210,
            '/tools/2/nozzle_temp': 40,
            '/tools/0/active': false,
            '/tools/2/active': true,
          },
          ['/tools/2/nozzle_temp'],
        ),
        device,
      ),
    ).toEqual([
      { index: 0, active: false, temperature: 210, features: {} },
      { index: 2, active: true, temperature: 40, features: {} },
    ]);
  });

  it('reads the feeder slots as part of their head, and leaves out a disabled head', () => {
    const device = fakeDevice('mk4s');
    device.state = {
      '/tools/0/active': true,
      '/tools/0.3/active': true,
      '/tools/0/nozzle_temp': 215,
      '/tools/0/feeder/slot_count': 5,
      [`/tools/0.3/${MATERIAL}`]: 'PETG',
      '/tools/1/enabled': false,
    };

    expect(toolsOf(live({}), device)).toEqual([
      {
        index: 0,
        active: true,
        temperature: 215,
        material: 'PETG',
        features: {},
      },
    ]);
    expect(activeHeadOf(live({}), device)).toBe(0);
  });

  // A head's own target and its nozzle, as the tree carries them. A head
  // whose heater is off has a target of 0, and reads as no target.
  it("reads each head's target and nozzle features", () => {
    const device = fakeDevice('xl');
    device.state = {
      '/tools/0/nozzle_target_temp': 215,
      '/tools/0/features/nozzle_diameter': 0.6,
      '/tools/0/features/nozzle_high_flow': true,
      '/tools/0/features/nozzle_hardened': false,
      '/tools/1/enabled': true,
      '/tools/1/nozzle_target_temp': 0,
    };

    expect(toolsOf(live({}), device)).toEqual([
      {
        index: 0,
        active: false,
        target: 215,
        features: { diameter: 0.6, highFlow: true, hardened: false },
      },
      { index: 1, active: false, features: {} },
    ]);
  });

  // The architecture marks each tool: a head while it is picked, and a slot
  // while it is loaded into its head, even a head that is not picked.
  it('reads the active member of each head and the slot loaded into it', () => {
    const device = fakeDevice('xl');
    device.state = {
      '/tools/0/active': true,
      '/tools/0/feeder/slot_count': 2,
      '/tools/0.1/active': true,
      [`/tools/0.1/${MATERIAL}`]: 'PLA',
      '/tools/1/active': false,
      '/tools/1/feeder/slot_count': 2,
      '/tools/1.0/active': true,
      [`/tools/1.0/${MATERIAL}`]: 'PETG',
      '/tools/1.1/active': false,
      [`/tools/1.1/${MATERIAL}`]: 'ASA',
    };

    expect(toolsOf(live({}), device)).toEqual([
      { index: 0, active: true, material: 'PLA', features: {} },
      { index: 1, active: false, material: 'PETG', features: {} },
    ]);

    // The live stream moves the pick to head 1 before the snapshot does.
    expect(
      toolsOf(
        live({ '/tools/0/active': false, '/tools/1/active': true }),
        device,
      ).map((tool) => tool.active),
    ).toEqual([false, true]);
  });

  it('answers nothing for a printer that reports no tools', () => {
    expect(toolsOf(live({}), fakeDevice('mk4'))).toEqual([]);
  });

  it('reads no head from the paths an earlier driver wrote', () => {
    const device = fakeDevice('xl');
    device.state = {
      '/slot/1/temperature': 100,
      '/tool/0/temperature': 1,
      '/tools/00/nozzle_temp': 1,
      '/active_tool': 1,
    };

    expect(toolsOf(live({}), device)).toEqual([]);
    expect(activeHeadOf(live({}), device)).toBeUndefined();
  });

  it('sends a head index as the tool argument, counted from 0', () => {
    expect(toolArgument(0)).toBe('0');
    expect(toolArgument(4)).toBe('4');
  });
});

describe('activeMaterialOf', () => {
  const live = (values: Record<string, unknown>) => ({
    value: (path: string) => values[path],
  });
  const MATERIAL = 'material_package_instance/package/material/abbreviation';

  it('reads the material of the tool printing now from the tree', () => {
    const device = fakeDevice('mk4s');
    device.state = {
      '/tools/0/active': true,
      '/tools/0.1/active': true,
      '/tools/0/material_package_instance/package/material/abbreviation': 'PLA',
      [`/tools/0.1/${MATERIAL}`]: 'PETG',
    };

    expect(activeMaterialOf(live({}), device)).toBe('PETG');
    expect(
      activeMaterialOf(live({ [`/tools/0.1/${MATERIAL}`]: 'ASA' }), device),
    ).toBe('ASA');
  });

  // An empty word in the tree is a tool the printer says is empty.
  it('reads an empty tool as nothing loaded', () => {
    const device = fakeDevice('mk4s');
    device.state = {
      '/tools/0/active': true,
      [`/tools/0/${MATERIAL}`]: '',
    };

    expect(activeMaterialOf(live({}), device)).toBeUndefined();
  });

  it('falls back to the flat alias where the tree does not say', () => {
    const device = fakeDevice('mk4');
    device.state = {
      '/tools/0/material_package_instance/package/material/abbreviation': 'PLA',
    };

    expect(activeMaterialOf(live({}), device)).toBe('PLA');
    expect(
      activeMaterialOf(
        live({
          '/tools/0/material_package_instance/package/material/abbreviation':
            'PETG',
        }),
        device,
      ),
    ).toBe('PETG');
  });
});

describe('toolArgument', () => {
  it('counts a head from 0', () => {
    expect(toolArgument(0)).toBe('0');
    expect(toolArgument(4)).toBe('4');
  });
});
