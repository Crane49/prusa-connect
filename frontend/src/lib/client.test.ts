import { describe, expect, it } from 'vitest';

import { paramsForLog } from './client';

describe('paramsForLog', () => {
  it('keeps the value of an identifier key and only the name of any other', () => {
    expect(
      paramsForLog({
        verb: 'set_temperature',
        argument: 'target',
        printer_name: 'Bob',
        min: 3,
        state: 'two words',
        field: 'x'.repeat(65),
      }),
    ).toEqual({
      verb: 'set_temperature',
      argument: 'target',
      printer_name: null,
      min: null,
      state: null,
      field: null,
    });
  });
});
