// How much room one printer gets, which a small instance and a farm answer
// differently.

/** What the screen draws: one printer large, two side by side, or the fleet. */
export type HomeShape = 'single' | 'pair' | 'fleet';

/**
 * What the reader asked for: the dashboard, `auto` being "read the count",
 * one printer, or the list (ADR-20261008-sww rule 13).
 */
export type HomePreference =
  'dashboard' | 'auto' | 'my-printer' | 'all-printers';

/**
 * The home a reader gets on this server. A server that leaves dashboards out
 * reads the dashboard as `auto`, so a reader who chose it on another server
 * still lands somewhere that exists (ADR-20261008-sww).
 */
export const homeOn = (
  preference: HomePreference,
  dashboards: boolean,
): HomePreference =>
  preference === 'dashboard' && !dashboards ? 'auto' : preference;

export const homeShapeFor = ({
  printerCount,
  preference = 'auto',
}: {
  printerCount: number;
  preference?: HomePreference;
}): HomeShape => {
  // Checked before the preference: there is nothing to pin a home to.
  if (printerCount === 0) return 'fleet';

  // With the dashboard as the home page, the printers page is the list.
  if (preference === 'all-printers' || preference === 'dashboard')
    return 'fleet';
  if (preference === 'my-printer') return 'single';

  if (printerCount === 1) return 'single';
  if (printerCount === 2) return 'pair';

  return 'fleet';
};
