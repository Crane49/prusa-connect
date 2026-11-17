// The dashboard a screen draws, whether the core keeps it or the home page
// wrote it from the fleet (ADR-20261008-sww rule 12).
import type { Dashboard } from '../../data';
import type { DashboardDraft } from '../../domain';

/** A dashboard on screen. One written from the fleet has no id until saved. */
export interface ShownDashboard extends DashboardDraft {
  readonly id: number | undefined;
  readonly revision: number;
}

/** A stored dashboard as the screen draws it, every optional field filled. */
export const shownOf = (
  dashboard: Dashboard,
): ShownDashboard & { readonly id: number } => ({
  id: dashboard.id,
  revision: dashboard.revision,
  name: dashboard.name,
  visibility: dashboard.visibility,
  columns: dashboard.columns,
  density: dashboard.density,
  cards: dashboard.cards.map((card) => ({
    id: card.id,
    type: card.type,
    x: card.x,
    y: card.y,
    w: card.w,
    h: card.h,
    title: card.title ?? '',
    config: card.config ?? {},
  })),
});
