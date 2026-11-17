// The printers as every tile reads them, worked out once for the whole page.
//
// The host's fleet replaces its device records on every change, and a page
// may hold several tiles that read every printer. Each of them flattening the
// fleet again would cost tiles times printers on every tick. So the views are
// derived here, once, and each tile filters the one list (svelte-data.md,
// derived values live in the store). A class, because every tile shares it.
import type { DeviceIssues } from '../../data';
import type { ReadonlyFleet } from '../../domain';

import { activeOf, type PrinterView, tagsOf, viewOf } from './printers';

/** One choice in a form: a value and the words that name it. */
export interface Option {
  readonly value: string;
  readonly label: string;
}

export class FleetViews {
  readonly #fleet: ReadonlyFleet;
  readonly #issues: Pick<DeviceIssues, 'entryOf'> | undefined;

  /**
   * `issues` is the server's vocabulary of issue reasons, the one the
   * Printers screen reads. Without it a card shows what the machine said.
   */
  constructor(fleet: ReadonlyFleet, issues?: Pick<DeviceIssues, 'entryOf'>) {
    this.#fleet = fleet;
    this.#issues = issues;
  }

  /** True once the fleet answered, which tells no printer from none read yet. */
  get loaded(): boolean {
    return this.#fleet.loaded;
  }

  /** Why the fleet could not be read, while it could not. */
  get error(): Error | undefined {
    return this.#fleet.error;
  }

  /** Every printer a tile may show, as a tile reads it. Read-only by design. */
  // Each reads the fleet inside a function, which runs on the first read and
  // never before the constructor has set the fleet.
  readonly views: readonly PrinterView[] = $derived.by(() => {
    const issues = this.#issues;
    const issueOf =
      issues === undefined
        ? undefined
        : (reason: string) => issues.entryOf(reason);

    return activeOf(this.#fleet.devices).map((device) =>
      viewOf(device, issueOf),
    );
  });

  /** Every tag the fleet uses, for a form. */
  readonly tags: readonly Option[] = $derived.by(() =>
    tagsOf(activeOf(this.#fleet.devices)).map((tag) => ({
      value: tag,
      label: tag,
    })),
  );

  /** Every printer by name, for a form. */
  readonly printers: readonly Option[] = $derived(
    this.views
      .map((view) => ({ value: view.id, label: view.name }))
      .sort((a, b) => a.label.localeCompare(b.label)),
  );

  /** One printer, or undefined for one this person cannot see. */
  view(id: string): PrinterView | undefined {
    return this.views.find((view) => view.id === id);
  }
}
