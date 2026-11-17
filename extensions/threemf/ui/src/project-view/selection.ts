// What a set of ticked copies of a 3MF project adds up to, and the shape a
// print of them takes. Plain functions, so the browser derives them and a
// test reads them without a component.
import {
  type BedRef,
  bedKey,
  type CopyRef,
  type Project,
  type ProjectInstance,
} from './model';

/** One ticked copy, with the copy itself. */
export interface TickedCopy extends CopyRef {
  readonly copy: ProjectInstance;
}

/**
 * The copies to print from one bed, in the shape `the threemf Go package` takes a
 * selection: the bed, and the build items by `Instance.item`.
 */
export interface BedSelection {
  readonly bed: BedRef;
  readonly items: readonly number[];
}

/** What a page's buttons for the ticked copies are handed. */
export interface ProjectSelectionActs {
  /** The ticked copies, one selection per bed. */
  readonly selections: readonly BedSelection[];
  readonly summary: SelectionSummary;
  /** Unticks every copy, for a button that is done with them. */
  readonly clear: () => void;
}

/** What the ticked copies add up to. */
export interface SelectionSummary {
  readonly copies: number;
  readonly objects: number;
  readonly beds: readonly BedRef[];
  /** The extruders the ticked objects print with, counted from 1, ascending. */
  readonly extruders: readonly number[];
}

const byBed = (a: BedRef, b: BedRef): number =>
  a.setup - b.setup || a.bed - b.bed;

export const summarise = (
  project: Project,
  ticked: readonly TickedCopy[],
): SelectionSummary => {
  const objects = new Set<number>();
  const beds = new Map<string, BedRef>();
  const extruders = new Set<number>();
  for (const row of ticked) {
    objects.add(row.object);
    if (row.copy.bed !== undefined)
      beds.set(bedKey(row.copy.bed), row.copy.bed);
    for (const extruder of project.objects[row.object]?.extruders ?? [1])
      extruders.add(extruder);
  }

  return {
    copies: ticked.length,
    objects: objects.size,
    beds: [...beds.values()].sort(byBed),
    extruders: [...extruders].sort((a, b) => a - b),
  };
};

/** The ticked copies as one selection per bed, beds and items ascending. */
export const selectionsOf = (
  ticked: readonly TickedCopy[],
): readonly BedSelection[] => {
  const beds = new Map<string, { bed: BedRef; items: number[] }>();
  for (const row of ticked) {
    const bed = row.copy.bed;
    if (bed === undefined) continue;
    const entry = beds.get(bedKey(bed)) ?? { bed, items: [] };
    entry.items.push(row.copy.item);
    beds.set(bedKey(bed), entry);
  }

  return [...beds.values()]
    .sort((a, b) => byBed(a.bed, b.bed))
    .map((entry) => ({
      bed: entry.bed,
      items: entry.items.sort((a, b) => a - b),
    }));
};
