// What a person does to a release, from a row's menu or from the detail: an
// install, a removal, the default. An install of a release with no published
// checksum and a removal are asked about first, in one dialog the page holds,
// so a row's menu can close while the question stays open.
import type { ReleaseRow } from './releases';
import type { SlicerView } from './store.svelte';

/** The question open on screen, if one is. */
export interface Asking {
  kind: 'unverified' | 'remove';
  version: string;
}

export class ReleaseActs {
  asking = $state<Asking | undefined>(undefined);

  readonly #view: SlicerView;

  constructor(view: SlicerView) {
    this.#view = view;
  }

  /** Installs at once, or asks first when no checksum was published. */
  install(row: ReleaseRow): void {
    if (row.digest === 'unverified') {
      this.asking = { kind: 'unverified', version: row.version };
      return;
    }
    void this.#view.install(row.version, false);
  }

  remove(row: ReleaseRow): void {
    this.asking = { kind: 'remove', version: row.version };
  }

  pin(row: ReleaseRow): void {
    void this.#view.pin(row.version);
  }

  /** Gives jobs the newest version again. */
  unpin(): void {
    void this.#view.pin('');
  }

  /** The person said yes to the open question. */
  confirm(): void {
    const asking = this.asking;
    this.asking = undefined;
    if (asking?.kind === 'unverified') {
      void this.#view.install(asking.version, true);
    } else if (asking?.kind === 'remove') {
      void this.#view.remove(asking.version);
    }
  }

  dismiss(): void {
    this.asking = undefined;
  }
}

/** What a person may do to a row, given who they are. */
export const canInstall = (row: ReleaseRow, administrator: boolean): boolean =>
  administrator &&
  (row.state === 'available' || row.state === 'failed') &&
  row.offer?.compatible === true;

export const canPin = (row: ReleaseRow, administrator: boolean): boolean =>
  administrator && row.state === 'installed' && !row.isDefault;

export const canUnpin = (row: ReleaseRow, administrator: boolean): boolean =>
  administrator && row.isPinned;

export const canRemove = (row: ReleaseRow, administrator: boolean): boolean =>
  administrator && row.state === 'installed' && row.removable;
