// What a person does while browsing one 3MF project: which bed the plan
// shows, which object the pointer is on, which object is open in its panel,
// and which copies are ticked.
//
// It is a class, because the plan, the object list, the selection bar and the
// object panel all read it (svelte-5.md). Each project view makes its own
// and hands it down through context, so two views on one page never share a
// selection.
import { Selection } from '@prusa/connect-ui/primitives';
import { getContext, setContext } from 'svelte';

import {
  type BedRef,
  bedKey,
  copiesIn,
  type CopyRef,
  instanceKey,
  isSelectable,
  type PlanFocus,
  type Project,
  type ProjectInstance,
} from './model';
import {
  type BedSelection,
  type SelectionSummary,
  selectionsOf,
  summarise,
  type TickedCopy,
} from './selection';

export type { BedSelection, SelectionSummary } from './selection';

/** The plan of every bed at once. */
export const ALL_BEDS = 'all';
/** The copies that stand on no bed. */
export const OFF_BEDS = 'off';

export class ProjectBrowser {
  readonly #project: () => Project;
  readonly #onopen: ((object: number) => void) | undefined;
  /**
   * Whether a person ticks copies here. A page that does nothing with the
   * ticked copies, such as a file's page, browses without ticking: a press
   * on a copy opens its object.
   */
  readonly ticking: boolean;

  #view = $state<string | undefined>(undefined);
  #hovered = $state<number | undefined>(undefined);
  #active = $state<number | undefined>(undefined);
  #opened = $state<number | undefined>(undefined);

  /**
   * The ticked copies, by `instanceKey`. It spans every bed view of the
   * project, so a tick survives a change of view. Only a copy that stands
   * whole on a bed can be ticked.
   */
  readonly selection: Selection<string>;

  /**
   * @param project reads the project the view draws now.
   * @param onopen is told each object a person opens, so the page can ask the
   * server for what the panel draws of it.
   * @param onselect is told the ticked copies each time a person changes
   * them, one selection per bed, so a page that embeds the view keeps its own
   * copy without reading the browser.
   * @param ticking is whether a person ticks copies at all.
   */
  constructor(
    project: () => Project,
    onopen?: (object: number) => void,
    onselect?: (selections: readonly BedSelection[]) => void,
    ticking = true,
  ) {
    this.#project = project;
    this.#onopen = onopen;
    this.ticking = ticking;
    this.selection = new Selection<string>({
      rows: () => this.#selectable,
      onchange: () => onselect?.(this.selections),
    });
  }

  /** The key of every copy that can be ticked, in project order. */
  readonly #selectable: readonly string[] = $derived.by(() =>
    this.#project().objects.flatMap((object, o) =>
      object.instances.flatMap((copy, i) =>
        isSelectable(copy) ? [instanceKey(o, i)] : [],
      ),
    ),
  );

  /** Every bed of the project, setup by setup. */
  readonly #beds: readonly BedRef[] = $derived.by(() =>
    this.#project().setups.flatMap((setup, s) =>
      setup.beds.map((_, b) => ({ setup: s, bed: b })),
    ),
  );

  /** Whether some copy stands on no bed, which gives the plan a view of them. */
  readonly #hasOffBed: boolean = $derived.by(() =>
    this.#project().objects.some((object) =>
      object.instances.some((instance) => instance.bed === undefined),
    ),
  );

  /** The views the plan can show, by key, in the order the switcher lists them. */
  readonly #views: readonly string[] = $derived.by(() => [
    ...(this.#beds.length > 1 ? [ALL_BEDS] : []),
    ...this.#beds.map(bedKey),
    ...(this.#hasOffBed ? [OFF_BEDS] : []),
  ]);

  // The view a project opens on: the first bed with a copy that prints, else
  // the first bed with any copy, else the first view there is.
  readonly #firstView: string = $derived.by(() => {
    const project = this.#project();
    const holds = (printing: boolean) =>
      this.#beds.find((ref) =>
        project.setups[ref.setup]?.beds[ref.bed]?.instances.some(
          (at) =>
            !printing ||
            project.objects[at.object]?.instances[at.instance]?.printable ===
              true,
        ),
      );
    const first = holds(true) ?? holds(false);

    return first === undefined ? (this.#views[0] ?? ALL_BEDS) : bedKey(first);
  });

  /** The view the plan shows, by key: a bed's key, `all` or `off`. */
  get view(): string {
    const view = this.#view;

    return view !== undefined && this.#views.includes(view)
      ? view
      : this.#firstView;
  }

  /** A key the project has no view for is refused and the view stays. */
  set view(next: string) {
    if (!this.#views.includes(next)) return;
    this.#view = next;
    this.#hovered = undefined;
  }

  /** What the plan draws for the view. */
  readonly #focus: PlanFocus = $derived.by(() => {
    const view = this.view;
    if (view === ALL_BEDS) return undefined;
    if (view === OFF_BEDS) return 'off';

    return this.#beds.find((ref) => bedKey(ref) === view);
  });

  /** The copies the view shows. */
  readonly #copies: readonly CopyRef[] = $derived.by(() =>
    copiesIn(this.#project(), this.#focus),
  );

  /** The object under the pointer, on the plan or in the list. */
  get hovered(): number | undefined {
    return this.#hovered;
  }

  set hovered(object: number | undefined) {
    if (object !== undefined && !this.#hasObject(object)) return;
    this.#hovered = object;
  }

  /** The object last pressed on the plan, which the list scrolls to and marks. */
  get active(): number | undefined {
    return this.#active;
  }

  set active(object: number | undefined) {
    if (object !== undefined && !this.#hasObject(object)) return;
    this.#active = object;
  }

  /** The object shown in its own panel. */
  get opened(): number | undefined {
    return this.#opened;
  }

  set opened(object: number | undefined) {
    if (object !== undefined && !this.#hasObject(object)) return;
    this.#opened = object;
    if (object !== undefined) this.#onopen?.(object);
  }

  /** Whether a copy can be ticked at all. */
  canSelect(object: number, instance: number): boolean {
    const copy = this.#copy(object, instance);

    return copy !== undefined && isSelectable(copy);
  }

  /** The keys of the copies given that can be ticked: what a group box covers. */
  keysOf(copies: readonly CopyRef[]): readonly string[] {
    return copies
      .filter((copy) => this.canSelect(copy.object, copy.instance))
      .map((copy) => instanceKey(copy.object, copy.instance));
  }

  isSelected(object: number, instance: number): boolean {
    return this.selection.has(instanceKey(object, instance));
  }

  /** Ticks a copy, or clears its tick. A copy that cannot be ticked is left. */
  toggle(object: number, instance: number): void {
    this.setCopies([{ object, instance }], !this.isSelected(object, instance));
  }

  /** Ticks every copy given that can be ticked, or clears every one given. */
  setCopies(copies: readonly CopyRef[], on: boolean): void {
    this.selection.set(this.keysOf(copies), on);
  }

  /**
   * Whether every copy given that can be ticked is ticked, some are, or none.
   * A set with nothing to tick reads as none.
   */
  stateOf(copies: readonly CopyRef[]): 'all' | 'some' | 'none' {
    return this.selection.stateOf(this.keysOf(copies));
  }

  clear(): void {
    this.selection.clear();
  }

  /** The ticked copies, object by object. */
  readonly #ticked: readonly TickedCopy[] = $derived.by(() => {
    const project = this.#project();
    const out: TickedCopy[] = [];
    project.objects.forEach((object, o) => {
      object.instances.forEach((copy, i) => {
        if (this.selection.has(instanceKey(o, i)))
          out.push({ object: o, instance: i, copy });
      });
    });

    return out;
  });

  readonly #summary: SelectionSummary = $derived.by(() =>
    summarise(this.#project(), this.#ticked),
  );

  /**
   * The ticked copies as one selection per bed, the shape a later act that
   * prints or slices them takes.
   */
  readonly #selections: readonly BedSelection[] = $derived.by(() =>
    selectionsOf(this.#ticked),
  );

  get beds(): readonly BedRef[] {
    return this.#beds;
  }

  get hasOffBed(): boolean {
    return this.#hasOffBed;
  }

  get views(): readonly string[] {
    return this.#views;
  }

  get focus(): PlanFocus {
    return this.#focus;
  }

  get copies(): readonly CopyRef[] {
    return this.#copies;
  }

  get summary(): SelectionSummary {
    return this.#summary;
  }

  get selections(): readonly BedSelection[] {
    return this.#selections;
  }

  #hasObject(object: number): boolean {
    return this.#project().objects[object] !== undefined;
  }

  #copy(object: number, instance: number): ProjectInstance | undefined {
    return this.#project().objects[object]?.instances[instance];
  }
}

const KEY = Symbol('project-browser');

/** Hands the view's browser to every part under it. */
export const setProjectBrowser = (browser: ProjectBrowser): ProjectBrowser =>
  setContext(KEY, browser);

/** The browser of the project view this part is drawn in. */
export const getProjectBrowser = (): ProjectBrowser => {
  const browser = getContext<ProjectBrowser | undefined>(KEY);
  if (browser === undefined) {
    throw new Error('a project part is drawn outside a project view');
  }

  return browser;
};
