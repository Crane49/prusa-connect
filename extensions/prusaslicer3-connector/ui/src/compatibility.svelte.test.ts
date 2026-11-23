// The cloud's rules for which printer takes a file, against cases written
// out by hand.
import { describe, expect, it } from 'vitest';

import {
  candidatesFor,
  incompatibilities,
  normalizeRequest,
  preselected,
  printMessage,
} from './compatibility';
import { farm, mk4sRequest, printer } from './test/fakes';

describe('the request from PrusaSlicer', () => {
  it('reads the per-tool fields of PrusaSlicer 2.8.1 and later', () => {
    expect(
      normalizeRequest({
        printerUuid: 'p1',
        filename: 'a.bgcode',
        printerModel: 'XL',
        material: ['PLA', 'PETG'],
        nozzle_diameter: [0.4, 0.6],
        filament_abrasive: [false, true],
        high_flow: [false, false],
        multiple_beds: true,
      }),
    ).toEqual({
      printerUuid: 'p1',
      filename: 'a.bgcode',
      printerModel: 'XL',
      materials: ['PLA', 'PETG'],
      nozzleDiameters: [0.4, 0.6],
      hardened: [false, true],
      highFlow: [false, false],
      multipleBeds: true,
    });
  });

  it('reads one material joined with ; and one nozzle diameter', () => {
    expect(
      normalizeRequest({ material: 'PLA;PETG', nozzleDiameter: 0.4 }),
    ).toEqual({ materials: ['PLA', 'PETG'], nozzleDiameters: [0.4] });
  });
});

describe('what keeps a printer from suiting the file', () => {
  it('is nothing for a printer of the model with the nozzle and material', () => {
    expect(
      incompatibilities(mk4sRequest, printer('a', 'A', 'MK4S', 'READY')),
    ).toEqual([]);
  });

  it('compares the model without case', () => {
    expect(
      incompatibilities(
        { printerModel: 'mk4s' },
        printer('a', 'A', 'MK4S', 'READY'),
      ),
    ).toEqual([]);
  });

  it('names each way the printer differs', () => {
    const other = printer('a', 'A', 'MK4', 'READY', [
      { nozzle: 0.6, material: 'PETG', hardened: false },
    ]);

    expect(
      incompatibilities({ ...mk4sRequest, hardened: [true] }, other),
    ).toEqual([
      'printerModel',
      'material',
      'nozzleDiameter',
      'filamentAbrasive',
    ]);
  });

  it('does not judge the material of a printer that reports none', () => {
    const empty = printer('a', 'A', 'MK4S', 'READY', [{ nozzle: 0.4 }]);

    expect(incompatibilities(mk4sRequest, empty)).toEqual([]);
  });

  it('takes a nozzle diameter that any tool has', () => {
    const xl = printer('x', 'X', 'XL', 'READY', [
      { nozzle: 0.4, material: 'PLA' },
      { nozzle: 0.6, material: 'PLA' },
    ]);

    expect(
      incompatibilities({ printerModel: 'XL', nozzleDiameters: [0.6] }, xl),
    ).toEqual([]);
  });
});

describe('the printers offered', () => {
  const offered = candidatesFor(mk4sRequest, farm);

  it('leaves out another model and puts the printers that can print first', () => {
    expect(offered.map((item) => item.printer.uuid)).toEqual([
      'mk4s-1',
      'mk4s-3',
      'mk4s-2',
      'mk4s-4',
    ]);
    expect(offered.map((item) => item.printable)).toEqual([
      true,
      true,
      false,
      false,
    ]);
  });

  it('picks the printer the slicer named when it can print', () => {
    expect(preselected(mk4sRequest, offered)).toBe('mk4s-1');
  });

  it('picks no printer the slicer named that cannot print', () => {
    expect(
      preselected({ ...mk4sRequest, printerUuid: 'mk4s-2' }, offered),
    ).toBeUndefined();
  });

  it('picks the only printer offered', () => {
    const one = candidatesFor({ printerModel: 'XL' }, farm);

    expect(preselected({ printerModel: 'XL' }, one)).toBe('xl-1');
  });
});

describe('the print message', () => {
  it('starts the print now, flat and under data', () => {
    const data = {
      set_ready: true,
      position: 0,
      wait_until: undefined,
      filename: 'a.bgcode',
      printer_uuid: 'p1',
      team_id: 1,
    };

    expect(printMessage('p1', 'a.bgcode')).toEqual({
      action: 'PRINT',
      ...data,
      data,
    });
  });
});
