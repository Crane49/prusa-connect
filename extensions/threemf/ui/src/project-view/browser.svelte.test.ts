import { flushSync } from 'svelte';
import { describe, expect, it } from 'vitest';

import { multibed, parked } from '../test/project-fixtures';
import { mmuObjects } from '../test/project-fixtures-multimaterial';
import { ALL_BEDS, OFF_BEDS, ProjectBrowser } from './browser.svelte';
import { copiesIn, type Project } from './model';

const browse = (project: Project) => {
  let browser: ProjectBrowser | undefined;
  const dispose = $effect.root(() => {
    browser = new ProjectBrowser(() => project);
  });
  if (browser === undefined) throw new Error('no browser');

  return { browser, dispose };
};

describe('ProjectBrowser', () => {
  it('opens on the first bed that prints, and lists every view', () => {
    const { browser, dispose } = browse(multibed);

    expect(browser.views).toEqual([ALL_BEDS, '0:0', '0:1', '0:2']);
    expect(browser.view).toBe('0:0');
    expect(browser.focus).toEqual({ setup: 0, bed: 0 });
    dispose();
  });

  it('gives the copies on no bed a view of their own', () => {
    const { browser, dispose } = browse(parked);

    expect(browser.views.at(-1)).toBe(OFF_BEDS);
    browser.view = OFF_BEDS;
    flushSync();
    expect(browser.focus).toBe('off');
    expect(browser.copies).toEqual([{ object: 5, instance: 0 }]);
    dispose();
  });

  it('refuses a view the project does not have', () => {
    const { browser, dispose } = browse(multibed);

    browser.view = '0:1';
    browser.view = '3:0';
    expect(browser.view).toBe('0:1');
    dispose();
  });

  it('refuses an object the project does not have', () => {
    const { browser, dispose } = browse(multibed);

    browser.opened = 1;
    browser.opened = 99;
    browser.active = -1;
    expect(browser.opened).toBe(1);
    expect(browser.active).toBeUndefined();
    dispose();
  });

  it('ticks only copies that stand whole on a bed', () => {
    const { browser, dispose } = browse(parked);

    browser.setCopies(copiesIn(parked, undefined), true);

    // The sign on no bed and the copy half off its bed are left.
    expect(browser.isSelected(5, 0)).toBe(false);
    expect(browser.isSelected(2, 0)).toBe(false);
    expect(browser.canSelect(5, 0)).toBe(false);
    expect(browser.selection.count).toBe(parked.counts.instances - 2);
    expect(browser.stateOf(copiesIn(parked, undefined))).toBe('all');
    dispose();
  });

  it('says whether a set of copies is ticked whole, in part or not', () => {
    const { browser, dispose } = browse(multibed);
    const cubeA = multibed.objects[0]?.instances.map((_, i) => ({
      object: 0,
      instance: i,
    }));

    expect(browser.stateOf(cubeA ?? [])).toBe('none');
    browser.toggle(0, 0);
    expect(browser.stateOf(cubeA ?? [])).toBe('some');
    browser.setCopies(cubeA ?? [], true);
    expect(browser.stateOf(cubeA ?? [])).toBe('all');
    browser.clear();
    expect(browser.selection.count).toBe(0);
    dispose();
  });

  it('adds up the ticked copies and groups them by bed for a print', () => {
    const { browser, dispose } = browse(multibed);

    browser.setCopies(copiesIn(multibed, undefined), true);
    flushSync();

    expect(browser.summary.copies).toBe(multibed.counts.instances);
    expect(browser.summary.objects).toBe(multibed.counts.objects);
    const beds = multibed.setups[0]?.beds ?? [];
    const occupied = beds.flatMap((bed, b) =>
      bed.instances.length > 0 ? [{ setup: 0, bed: b }] : [],
    );
    expect(browser.summary.beds).toEqual(occupied);
    // One selection per bed, its build items ascending, each from a copy on
    // that bed.
    expect(browser.selections.map((row) => row.bed)).toEqual(occupied);
    for (const row of browser.selections) {
      const items = (beds[row.bed.bed]?.instances ?? []).map(
        (at) => multibed.objects[at.object]?.instances[at.instance]?.item ?? -1,
      );
      expect(row.items).toEqual(
        [...items].filter((item) => item >= 0).sort((a, b) => a - b),
      );
    }
    dispose();
  });

  it('counts the extruders of the ticked objects', () => {
    const { browser, dispose } = browse(mmuObjects);

    browser.toggle(0, 0);
    flushSync();
    expect(browser.summary.extruders).toEqual(mmuObjects.objects[0]?.extruders);
    dispose();
  });
});
