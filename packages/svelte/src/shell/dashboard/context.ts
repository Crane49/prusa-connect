// What every part of the dashboard screen reads, handed down once.
//
// The screen builds it from what the application gives it and sets it as
// Svelte context, so a card, the grid and the editor read the one set of
// stores without a prop passed through every layer (svelte-5.md, context for
// state per subtree). Two dashboards on one page, such as a test beside a
// preview, each get their own.
//
// A module's card reads none of this. It reads the host object it was given
// (ADR-20261008-sww rule 21), and the core's own cards read the same stores here.
import { getContext, setContext } from 'svelte';

import type { DashboardEditor, Dashboards, SharedTelemetry } from '../../data';
import type { LeaveCheck } from '../../router';
import type { CardCatalogue } from './card-catalogue';
import type { CameraFrames } from './cameras.svelte';
import type { Clock } from './clock.svelte';
import type { FleetViews } from './fleet-views.svelte';

/** Where the screen sends a person, written by the application. */
export interface DashboardNavigation {
  /**
   * Opens one dashboard. With `edit`, the screen it opens starts the editor
   * on it, which is how a dashboard just made opens ready to change. With
   * `replace`, the address takes the place of the current one in history.
   */
  open(id: number, options?: { edit?: boolean; replace?: boolean }): void;
  /** Draws the dashboard on screen over the whole screen, or stops. */
  kiosk(on: boolean): void;
  /** The whole address of one dashboard, origin included, for a link to copy. */
  link(id: number, kiosk: boolean): string;
  /**
   * The address of the dashboard list's own page, after a delete. The screen
   * is drawn anew even when that address is already on screen, so the home
   * page chooses again.
   */
  home(): void;
  /**
   * Asks `check` before a link or the back button leaves the page, and
   * returns the call that stops asking (router.svelte.ts, `checkLeave`).
   */
  checkLeave(check: LeaveCheck): () => void;
  /**
   * Opens the page that adds a printer. The screen offers it while the
   * server has no printer, and not at all when the application leaves it out.
   */
  addPrinter?(): void;
}

export interface DashboardContext {
  readonly dashboards: Dashboards;
  readonly editor: DashboardEditor;
  readonly printers: FleetViews;
  readonly telemetry: SharedTelemetry;
  readonly cameras: CameraFrames;
  readonly clock: Clock;
  readonly cards: CardCatalogue;
  readonly navigation: DashboardNavigation;
}

const KEY = Symbol('dashboard');

/** Sets the context for the dashboard screen drawn under the caller. */
export const setDashboardContext = (context: DashboardContext): void => {
  setContext(KEY, context);
};

/** What the dashboard screen set. It throws rather than drawing from nothing. */
export const dashboardContext = (): DashboardContext => {
  const context = getContext<DashboardContext | undefined>(KEY);
  if (context === undefined) {
    throw new Error(
      'a part of the dashboard was drawn outside the dashboard screen',
    );
  }

  return context;
};
