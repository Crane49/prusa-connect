import { describe, expect, it } from 'vitest';

import { gcodeOnly, mmuObjects } from '../test/project-fixtures-multimaterial';
import {
  generic,
  legacyRow,
  manyBeds,
  multibed,
  orcaPlates,
  bambuPlates,
  parked,
  twoSetups,
} from '../test/project-fixtures';
import {
  bedsOfObject,
  bedsWithGCode,
  filamentsFor,
  fittedLabel,
  isGCodeOnly,
  instanceState,
  byObject,
  copiesIn,
  isSelectable,
  mainExtruder,
  objectColour,
  planOf,
  printersOf,
  producerKind,
  producerText,
} from './model';

describe('the project model', () => {
  it('colours a copy by where it stands before its flag', () => {
    const [cubeA] = twoSetups.objects;
    expect(cubeA?.instances.map(instanceState)).toEqual([
      'printable',
      'nonPrintable',
      'printable',
      'printable',
    ]);
    expect(instanceState(twoSetups.objects[1]?.instances[2] ?? never())).toBe(
      'outside',
    );
    expect(
      instanceState({
        ...(twoSetups.objects[1]?.instances[2] ?? never()),
        placement: 'partly_outside',
        printable: false,
      }),
    ).toBe('nonPrintable');
    expect(
      instanceState({
        ...(twoSetups.objects[1]?.instances[2] ?? never()),
        placement: 'partly_outside',
      }),
    ).toBe('partlyOutside');
    expect(instanceState(generic.objects[0]?.instances[0] ?? never())).toBe(
      'unknown',
    );
  });

  it('tells the producers apart for the badge', () => {
    expect(producerKind(multibed.producer)).toBe('prusaslicer');
    expect(producerKind(twoSetups.producer)).toBe('prusaslicerPreview');
    expect(producerKind(orcaPlates.producer)).toBe('orcaslicer');
    expect(producerKind(bambuPlates.producer)).toBe('unsupported');
    expect(producerKind(generic.producer)).toBe('generic');
    expect(producerText(multibed.producer)).toBe('PrusaSlicer 2.9.6');
    expect(producerText({ dialect: 'generic', supported: true })).toBe('');
  });

  it('names each printer once, in setup order', () => {
    expect(printersOf(twoSetups)).toEqual(['COREONE', 'MINI']);
    expect(printersOf(generic)).toEqual([]);
  });

  it('lists the copies on one bed and groups them by object', () => {
    const copies = copiesIn(multibed, { setup: 0, bed: 0 });
    expect(
      byObject(copies).map((group) => [group.object, group.copies.length]),
    ).toEqual([
      [0, 2],
      [1, 1],
      [2, 1],
    ]);
    expect(copiesIn(multibed, undefined)).toHaveLength(
      multibed.counts.instances,
    );
  });

  it('draws an object in the colour of the extruder of its first part', () => {
    const object = mmuObjects.objects[0] ?? never();
    expect(mainExtruder(object)).toBe(object.parts[0]?.extruder);
    expect(objectColour(mmuObjects, object)).toMatch(/^#/);
    expect(objectColour(generic, generic.objects[0] ?? never())).toBe(
      undefined,
    );
  });

  it('ticks only a copy that stands whole on a bed', () => {
    const sign = parked.objects[5]?.instances[0] ?? never();
    const halfOff = parked.objects[2]?.instances[0] ?? never();
    expect(isSelectable(sign)).toBe(false);
    expect(isSelectable(halfOff)).toBe(false);
    expect(isSelectable(multibed.objects[0]?.instances[0] ?? never())).toBe(
      true,
    );
  });

  it('lists the beds of an object each once', () => {
    expect(bedsOfObject(multibed.objects[0] ?? never())).toEqual([
      { setup: 0, bed: 0 },
      { setup: 0, bed: 1 },
      { setup: 0, bed: 2 },
    ]);
  });
});

describe('the plan', () => {
  it('lays the beds out at their offsets with the back at the top', () => {
    const plan = planOf(legacyRow);
    expect(plan.beds.map((bed) => [bed.x, bed.y])).toEqual([
      [0, -210],
      [300, -210],
      [600, -210],
    ]);
    // Every bed and every copy is inside the drawing.
    for (const bed of plan.beds) {
      expect(bed.x).toBeGreaterThanOrEqual(plan.x);
      expect(bed.y).toBeGreaterThanOrEqual(plan.y);
      expect(bed.x + bed.width).toBeLessThanOrEqual(plan.x + plan.width);
      expect(bed.y + bed.height).toBeLessThanOrEqual(plan.y + plan.height);
    }
  });

  it('moves a copy by the offset of its bed', () => {
    const plan = planOf(legacyRow);
    const onSecond = plan.instances.find((copy) => copy.key === '0:1');
    expect(onSecond?.centreX).toBe(355);
    expect(onSecond?.centreY).toBe(-55);
  });

  it('draws one bed at the origin with only its own copies', () => {
    const plan = planOf(legacyRow, { setup: 0, bed: 1 });
    expect(plan.beds).toHaveLength(1);
    expect(plan.beds[0]?.x).toBe(0);
    expect(plan.instances.map((copy) => copy.key)).toEqual(['0:1']);
    expect(plan.instances[0]?.centreX).toBe(55);
  });

  it('lays a copy on no bed out beside the beds, never over one', () => {
    const plan = planOf(parked);
    const sign = plan.instances.find((copy) => copy.object === 5);
    const area = plan.offBed;
    expect(sign?.offBed).toBe(true);
    expect(sign?.state).toBe('nonPrintable');
    expect(sign?.placement).toBe('outside');
    expect(area).toBeDefined();
    const right = Math.max(...plan.beds.map((bed) => bed.x + bed.width));
    expect(area?.x).toBeGreaterThan(right);
    for (const bed of plan.beds) {
      const centreX = sign?.centreX ?? 0;
      expect(centreX < bed.x || centreX > bed.x + bed.width).toBe(true);
    }
    // One bed alone draws nothing off it.
    expect(planOf(parked, { setup: 0, bed: 0 }).offBed).toBeUndefined();
  });

  it('marks a bed that holds only parked copies', () => {
    const lettersOnly = {
      ...parked,
      objects: parked.objects.map((object) => ({
        ...object,
        instances: object.instances.map((instance) =>
          instance.bed?.bed === 2
            ? { ...instance, printable: false }
            : instance,
        ),
      })),
    };
    expect(planOf(lettersOnly).beds.map((bed) => bed.parkedOnly)).toEqual([
      false,
      false,
      true,
    ]);
  });

  it('marks a bed with nothing on it as empty', () => {
    expect(planOf(parked).beds.map((bed) => bed.empty)).toEqual([
      false,
      true,
      false,
    ]);
  });

  it('counts every copy once, a parked copy as not printable', () => {
    const states = parked.objects.flatMap((object) =>
      object.instances.map(instanceState),
    );
    expect(states).toHaveLength(parked.counts.instances);
    expect(states.filter((state) => state === 'nonPrintable')).toHaveLength(2);
  });

  it('draws a bed of unknown size around what stands on it', () => {
    const plan = planOf(generic);
    const [bed] = plan.beds;
    expect(bed?.known).toBe(false);
    expect(bed?.shape).toBeUndefined();
    for (const copy of plan.instances) {
      expect(copy.centreX).toBeGreaterThan(bed?.x ?? Infinity);
      expect(copy.centreX).toBeLessThan((bed?.x ?? 0) + (bed?.width ?? 0));
    }
  });

  it('draws every copy of a large project once', () => {
    const plan = planOf(manyBeds);
    expect(plan.beds).toHaveLength(9);
    expect(plan.instances).toHaveLength(manyBeds.counts.instances);
    expect(new Set(plan.instances.map((copy) => copy.key)).size).toBe(
      manyBeds.counts.instances,
    );
  });
});

describe('a label inside a copy', () => {
  it('fits the copy, is cut short, or is left out', () => {
    const wide = { labelWidth: 100, labelHeight: 20 };
    expect(fittedLabel('Bracket', wide, 4)).toBe('Bracket');
    expect(fittedLabel('Replacement bracket for the left door', wide, 4)).toBe(
      `${'Replacement bracket for the left door'.slice(0, 28)}…`,
    );
    expect(fittedLabel('Bracket', { labelWidth: 10, labelHeight: 20 }, 4)).toBe(
      '',
    );
    expect(fittedLabel('Bracket', { labelWidth: 100, labelHeight: 4 }, 4)).toBe(
      '',
    );
    // Room for four letters fits "Cube" whole and leaves "Bracket" unnamed.
    const four = { labelWidth: 14, labelHeight: 20 };
    expect(fittedLabel('Cube', four, 4)).toBe('Cube');
    expect(fittedLabel('Bracket', four, 4)).toBe('');
  });
});

function never(): never {
  throw new Error('the fixture lacks what the test reads');
}

describe('filamentsFor', () => {
  it('takes the filaments of the setup of the first copy on a bed', () => {
    expect(
      filamentsFor(mmuObjects, mmuObjects.objects[0]).map((f) => f.colour),
    ).toEqual(['#FF8000', '#DB5182', '#3EC0FF', '#FF4F4F', '#FBEB7D']);
  });

  it('takes the first setup for an object with no copy on a bed', () => {
    expect(filamentsFor(mmuObjects, undefined)).toHaveLength(5);
  });
});

describe('isGCodeOnly', () => {
  it('tells a file of G-code alone from a project', () => {
    expect(isGCodeOnly(gcodeOnly)).toBe(true);
    expect(bedsWithGCode(gcodeOnly)).toBe(1);
    expect(isGCodeOnly(mmuObjects)).toBe(false);
    expect(bedsWithGCode(mmuObjects)).toBe(0);
  });
});
