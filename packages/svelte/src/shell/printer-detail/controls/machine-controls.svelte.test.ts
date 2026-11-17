import { afterEach, describe, expect, it } from 'vitest';

import { PREHEAT_PRESETS } from '../../../domain';
import { formatting } from '../../../i18n';
import {
  BED_MAX_CELSIUS,
  bedTravelsOnZ,
  degreeScale,
  HOSTNAME_MAX,
  hostnameProblem,
  NOZZLE_MAX_CELSIUS,
} from './machine-controls';

afterEach(() => {
  formatting.temperature = 'celsius';
});

describe('degreeScale', () => {
  // Five degrees Celsius is nine Fahrenheit, so a slider step in either unit
  // lands on a whole figure in the other and sends back what it showed.
  it('steps in Fahrenheit by the nine degrees that make five Celsius', () => {
    formatting.temperature = 'fahrenheit';
    const scale = degreeScale();

    expect(scale.step).toBe(9);
    for (let shown = scale.shown(0); shown <= scale.shown(300); shown += 9) {
      const celsius = scale.celsius(shown);

      expect(Number.isInteger(celsius), `${String(shown)}°F`).toBe(true);
      expect(scale.shown(celsius)).toBe(shown);
    }
  });

  it('puts every limit and preset on a Fahrenheit step', () => {
    formatting.temperature = 'fahrenheit';
    const scale = degreeScale();
    const figures = [
      0,
      NOZZLE_MAX_CELSIUS,
      BED_MAX_CELSIUS,
      ...PREHEAT_PRESETS.flatMap((preset) => [
        preset.nozzleCelsius,
        preset.bedCelsius,
      ]),
    ];

    for (const celsius of figures) {
      expect((scale.shown(celsius) - scale.shown(0)) % scale.step).toBe(0);
    }
  });
});

describe('hostnameProblem', () => {
  it.each(['prusa-xl', 'XL2', 'a', '0', 'x'.repeat(HOSTNAME_MAX), ''])(
    'takes %j',
    (name) => {
      expect(hostnameProblem(name)).toBeUndefined();
    },
  );

  it.each(['-xl', 'xl-', 'prusa xl', 'prusa.local', 'tiskárna', 'xl_2'])(
    'refuses %j as outside one network label',
    (name) => {
      expect(hostnameProblem(name)).toBe(
        'printerDetailControls.hostname.invalid',
      );
    },
  );

  it('refuses a name longer than the printer stores', () => {
    expect(hostnameProblem('x'.repeat(HOSTNAME_MAX + 1))).toBe(
      'printerDetailControls.hostname.tooLong',
    );
  });
});

describe('bedTravelsOnZ', () => {
  it.each(['COREONE', 'CORE One', 'COREONEL', 'XL'])(
    '%s moves the bed',
    (m) => {
      expect(bedTravelsOnZ(m)).toBe(true);
    },
  );

  it.each(['MK4S', 'MK3.9', 'MINI', ''])('%s moves the nozzle', (m) => {
    expect(bedTravelsOnZ(m)).toBe(false);
  });
});
