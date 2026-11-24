// How long ago PrusaSlicer last reached a printer, in the largest whole unit,
// as the person's language says it: "now", "5 minutes ago", "2 days ago".
import { formatRelative } from '@prusa/connect-ui/i18n';

const MINUTE = 60_000;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

export const agoOf = (at: string, now: number): string => {
  const past = Math.max(0, now - Date.parse(at));

  if (past < MINUTE) return formatRelative(0, 'second');
  if (past < HOUR) return formatRelative(-Math.floor(past / MINUTE), 'minute');
  if (past < DAY) return formatRelative(-Math.floor(past / HOUR), 'hour');

  return formatRelative(-Math.floor(past / DAY), 'day');
};
