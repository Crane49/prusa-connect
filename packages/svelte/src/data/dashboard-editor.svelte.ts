// A dashboard while a person changes it: the draft, the steps back and
// forward, and the place a card would land while it is being dragged.
//
// A class, because the grid, the card frames, the settings panel and the
// head of the page all read and write the one draft (svelte-5.md). Nothing
// reaches the core until the person saves, so a change is tried on screen
// first and costs no request, and leaving without saving leaves the
// dashboard as it was.
//
// Every step is a new draft object, replaced whole and never changed in
// place. That makes a step back one assignment, and lets Svelte skip every
// card a step did not touch (svelte-testing.md, counting recomputations).
import {
  type CardConfig,
  type CellSize,
  type DashboardCard,
  type DashboardDensity,
  type DashboardDraft,
  type DashboardVisibility,
  fitsCardConfig,
  freeCell,
  freshCardId,
  MAX_CARDS,
  MAX_COLUMNS,
  MIN_COLUMNS,
  moveTile,
  placeCard,
  refitTiles,
  resizeTile,
  samePlaces,
  scaledSize,
  settleTiles,
  validCardTitle,
  validDashboardName,
  layoutFits,
} from '../domain';

/** How many steps back the editor keeps. */
const MAX_STEPS = 100;

/**
 * How long after one change of a card's words or settings the next one joins
 * the same step. Typing a note is then one step back, not one per letter.
 */
const JOIN_MS = 1500;

/** The smallest a card of a type may be, in cells of a 12-column grid. */
export type LeastOf = (type: string) => CellSize;

/**
 * A dashboard as the editor starts from it. One the home page wrote from the
 * fleet has no id: the core keeps it only when the person saves it.
 */
export interface EditedDashboard extends DashboardDraft {
  readonly id: number | undefined;
  readonly revision: number;
}

/** What the editor sends to the core: the draft and the revision it started from. */
export interface EditorRequest extends DashboardDraft {
  readonly revision: number;
}

export class DashboardEditor {
  readonly #leastOf: LeastOf;
  readonly #now: () => number;

  #editing = $state(false);
  #dashboard = $state<number | undefined>(undefined);
  #revision = 0;
  #steps = $state.raw<readonly DashboardDraft[]>([]);
  #at = $state(0);
  #saved: DashboardDraft | undefined;
  #preview = $state.raw<readonly DashboardCard[] | undefined>(undefined);
  #selected = $state<string | undefined>(undefined);
  /** The card the last step changed the words or settings of, and when. */
  #joining: { id: string; at: number } | undefined;

  constructor(leastOf: LeastOf, now: () => number = Date.now) {
    this.#leastOf = leastOf;
    this.#now = now;
  }

  /** True while a dashboard is open for changes. */
  get editing(): boolean {
    return this.#editing;
  }

  /** The dashboard the draft belongs to, undefined for one not stored yet. */
  get dashboard(): number | undefined {
    return this.#dashboard;
  }

  /** The draft as it stands. Read-only by design: the methods change it. */
  get draft(): DashboardDraft | undefined {
    return this.#steps[this.#at];
  }

  /** The cards to draw: the place a dragged card would land, else the draft. */
  get cards(): readonly DashboardCard[] {
    return this.#preview ?? this.draft?.cards ?? [];
  }

  /** True when the draft differs from what was last saved. */
  get dirty(): boolean {
    return this.#editing && this.draft !== this.#saved;
  }

  get canUndo(): boolean {
    return this.#at > 0;
  }

  get canRedo(): boolean {
    return this.#at < this.#steps.length - 1;
  }

  /** The card a person last picked, which the settings panel shows. */
  get selected(): string | undefined {
    return this.#selected;
  }

  /** Picks a card, or none. An id the draft does not hold picks none. */
  set selected(id: string | undefined) {
    this.#selected =
      id !== undefined && this.draft?.cards.some((card) => card.id === id)
        ? id
        : undefined;
  }

  /** One card of the draft. */
  card(id: string): DashboardCard | undefined {
    return this.draft?.cards.find((card) => card.id === id);
  }

  /** Opens a dashboard for changes, from what the core answered. */
  start(dashboard: EditedDashboard): void {
    const draft: DashboardDraft = {
      name: dashboard.name,
      visibility: dashboard.visibility,
      columns: dashboard.columns,
      density: dashboard.density,
      cards: dashboard.cards,
    };
    this.#dashboard = dashboard.id;
    this.#revision = dashboard.revision;
    this.#steps = [draft];
    this.#at = 0;
    this.#saved = draft;
    this.#preview = undefined;
    this.#selected = undefined;
    this.#joining = undefined;
    this.#editing = true;
  }

  /** Leaves the editor. What was not saved is dropped. */
  stop(): void {
    this.#editing = false;
    this.#dashboard = undefined;
    this.#steps = [];
    this.#at = 0;
    this.#saved = undefined;
    this.#preview = undefined;
    this.#selected = undefined;
    this.#joining = undefined;
  }

  /** The draft as the core takes it, against the revision it started from. */
  request(): EditorRequest | undefined {
    const draft = this.draft;
    if (draft === undefined) return undefined;

    return {
      ...draft,
      name: draft.name.trim(),
      cards: [...draft.cards],
      revision: this.#revision,
    };
  }

  undo(): void {
    if (!this.canUndo) return;
    this.#joining = undefined;
    this.#preview = undefined;
    this.#at -= 1;
    this.#keepSelection();
  }

  redo(): void {
    if (!this.canRedo) return;
    this.#joining = undefined;
    this.#preview = undefined;
    this.#at += 1;
    this.#keepSelection();
  }

  /** Names the dashboard. A name the core would refuse changes nothing. */
  rename(name: string): boolean {
    if (!validDashboardName(name) || this.draft?.name === name) return false;

    return this.#step((draft) => ({ ...draft, name }));
  }

  /** Says who sees the dashboard. */
  setVisibility(visibility: DashboardVisibility): boolean {
    if (this.draft?.visibility === visibility) return false;

    return this.#step((draft) => ({ ...draft, visibility }));
  }

  /** Gives the grid another width, keeping each card's share of it. */
  setColumns(columns: number): boolean {
    if (
      !Number.isInteger(columns) ||
      columns < MIN_COLUMNS ||
      columns > MAX_COLUMNS
    ) {
      return false;
    }
    if (this.draft?.columns === columns) return false;

    return this.#step((draft) => ({
      ...draft,
      columns,
      cards: refitTiles(draft.cards, draft.columns, columns, (card) =>
        scaledSize(this.#leastOf(card.type), columns),
      ),
    }));
  }

  setDensity(density: DashboardDensity): boolean {
    if (this.draft?.density === density) return false;

    return this.#step((draft) => ({ ...draft, density }));
  }

  /** Adds one card where it fits, picks it, and answers its id. */
  add(
    type: string,
    size: CellSize,
    config: CardConfig = {},
  ): string | undefined {
    const draft = this.draft;
    if (draft === undefined || draft.cards.length >= MAX_CARDS)
      return undefined;

    const card = placeCard(draft.cards, type, size, draft.columns, config);
    if (!this.#step((held) => ({ ...held, cards: [...held.cards, card] })))
      return undefined;
    this.#selected = card.id;

    return card.id;
  }

  /** Adds a copy of one card beside it, and picks the copy. */
  duplicate(id: string): string | undefined {
    const draft = this.draft;
    const source = this.card(id);
    if (draft === undefined || source === undefined) return undefined;
    if (draft.cards.length >= MAX_CARDS) return undefined;

    const { x, y } = freeCell(draft.cards, source.w, source.h, draft.columns);
    const copy: DashboardCard = {
      ...source,
      id: freshCardId(draft.cards),
      x,
      y,
      config: { ...source.config },
    };
    if (!this.#step((held) => ({ ...held, cards: [...held.cards, copy] })))
      return undefined;
    this.#selected = copy.id;

    return copy.id;
  }

  remove(id: string): boolean {
    if (this.card(id) === undefined) return false;
    if (this.#selected === id) this.#selected = undefined;

    return this.#step((draft) => ({
      ...draft,
      cards: settleTiles(
        draft.cards.filter((card) => card.id !== id),
        draft.columns,
      ),
    }));
  }

  /** Writes the heading over one card. One the core would refuse changes nothing. */
  retitle(id: string, title: string): boolean {
    if (!validCardTitle(title)) return false;
    if (this.card(id)?.title === title) return false;

    return this.#change(id, (card) => ({ ...card, title }));
  }

  /** Replaces the settings of one card. Settings over the core's limit change nothing. */
  configure(id: string, config: CardConfig): boolean {
    if (!fitsCardConfig(config)) return false;
    const held = this.card(id)?.config;
    if (held !== undefined && JSON.stringify(held) === JSON.stringify(config)) {
      return false;
    }

    return this.#change(id, (card) => ({ ...card, config: { ...config } }));
  }

  /** Moves one card to a cell, as one step. */
  move(id: string, x: number, y: number): boolean {
    this.#preview = undefined;

    return this.#place(
      moveTile(this.draft?.cards ?? [], id, x, y, this.#columns()),
    );
  }

  /** Gives one card a size, as one step. It never goes below what its type needs. */
  resize(id: string, w: number, h: number): boolean {
    this.#preview = undefined;
    const card = this.card(id);
    if (card === undefined) return false;
    const least = this.#least(card);

    return this.#place(
      resizeTile(
        this.draft?.cards ?? [],
        id,
        Math.max(w, least.w),
        Math.max(h, least.h),
        this.#columns(),
      ),
    );
  }

  /**
   * Shows where a dragged card would land, without a step. A place the core
   * would refuse keeps the last preview.
   */
  previewMove(id: string, x: number, y: number): void {
    const draft = this.draft;
    if (draft === undefined) return;
    this.#show(moveTile(draft.cards, id, x, y, draft.columns), draft.columns);
  }

  /** Shows what a resize would do, without a step. */
  previewResize(id: string, w: number, h: number): void {
    const draft = this.draft;
    const card = this.card(id);
    if (draft === undefined || card === undefined) return;
    const least = this.#least(card);
    this.#show(
      resizeTile(
        draft.cards,
        id,
        Math.max(w, least.w),
        Math.max(h, least.h),
        draft.columns,
      ),
      draft.columns,
    );
  }

  /** Takes the place the preview shows as one step. */
  commit(): void {
    const preview = this.#preview;
    this.#preview = undefined;
    if (preview !== undefined) this.#place(preview);
  }

  /** Drops the preview, as Escape does in the middle of a drag. */
  cancelPreview(): void {
    this.#preview = undefined;
  }

  #show(cards: DashboardCard[], columns: number): void {
    if (layoutFits(cards, columns)) this.#preview = cards;
  }

  #least(card: DashboardCard): CellSize {
    return scaledSize(this.#leastOf(card.type), this.#columns());
  }

  #columns(): number {
    return this.draft?.columns ?? 12;
  }

  /** Takes a new layout as one step, unless every card kept its place. */
  #place(cards: readonly DashboardCard[]): boolean {
    const draft = this.draft;
    if (draft === undefined || samePlaces(draft.cards, cards)) return false;

    return this.#step((held) => ({ ...held, cards }));
  }

  #change(id: string, change: (card: DashboardCard) => DashboardCard): boolean {
    if (this.card(id) === undefined) return false;

    return this.#step(
      (draft) => ({
        ...draft,
        cards: draft.cards.map((card) =>
          card.id === id ? change(card) : card,
        ),
      }),
      id,
    );
  }

  /**
   * Writes one step. A step that changes nothing is not written, and neither
   * is one whose layout the core would refuse, such as a card pushed below
   * the last row. A change of the same card's words soon after the last one
   * replaces that step rather than adding one, unless it is the step that
   * was saved.
   */
  #step(
    change: (draft: DashboardDraft) => DashboardDraft,
    join?: string,
  ): boolean {
    const draft = this.draft;
    if (!this.#editing || draft === undefined) return false;

    const next = change(draft);
    if (next === draft) return false;
    if (next.cards !== draft.cards && !layoutFits(next.cards, next.columns))
      return false;

    const now = this.#now();
    const joins =
      join !== undefined &&
      this.#joining?.id === join &&
      now - this.#joining.at < JOIN_MS &&
      this.#at > 0 &&
      draft !== this.#saved &&
      this.#at === this.#steps.length - 1;
    this.#joining = join === undefined ? undefined : { id: join, at: now };

    if (joins) {
      this.#steps = [...this.#steps.slice(0, this.#at), next];

      return true;
    }

    const kept = this.#steps.slice(
      Math.max(0, this.#at + 1 - MAX_STEPS),
      this.#at + 1,
    );
    this.#steps = [...kept, next];
    this.#at = this.#steps.length - 1;

    return true;
  }

  #keepSelection(): void {
    if (
      this.#selected !== undefined &&
      this.card(this.#selected) === undefined
    ) {
      this.#selected = undefined;
    }
  }
}
