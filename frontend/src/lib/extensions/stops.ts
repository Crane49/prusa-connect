// One sentence for why a module's process stopped, from the stop the host
// recorded. The switch-off rule is configurable, so its sentence names the
// failures and the window in force rather than a fixed rule.
import type { components } from '@prusa/connect';
import {
  formatBytes,
  formatDuration,
  formatNumber,
  type Translate,
} from '@prusa/connect-ui/i18n';

export type ResourceStop = components['schemas']['ResourceStop'];
export type ResourceMemory = components['schemas']['ResourceMemory'];

const KEY = 'app.settings.extensions.stop';
const HOUR = 3600;

// formatDuration writes a whole hour as "1 hr, 0 min".
export const windowText = (seconds: number): string =>
  seconds >= HOUR && seconds % HOUR === 0
    ? formatNumber(seconds / HOUR, {
        style: 'unit',
        unit: 'hour',
        unitDisplay: 'long',
      })
    : formatDuration(seconds);

/**
 * The reason of one stop. `memory` is the status the stop belongs to; the
 * switch-off sentence reads its record and window, and without them says only
 * that the module kept failing.
 */
export const stopText = (
  stop: ResourceStop,
  t: Translate,
  memory?: ResourceMemory,
): string => {
  switch (stop.result) {
    case 'over-memory':
      return stop.used_bytes !== undefined && stop.budget_bytes !== undefined
        ? t(`${KEY}.overMemory`, {
            used: formatBytes(stop.used_bytes),
            budget: formatBytes(stop.budget_bytes),
          })
        : t(`${KEY}.overMemoryPlain`);
    case 'not-responding':
      return t(`${KEY}.notResponding`);
    case 'exited':
      return stop.code === undefined
        ? t(`${KEY}.exitedPlain`)
        : t(`${KEY}.exited`, { code: stop.code });
    case 'signal':
      return stop.signal === undefined
        ? t(`${KEY}.signalPlain`)
        : t(`${KEY}.signal`, { signal: stop.signal });
    case 'stopped-by-operator':
      return t(`${KEY}.operator`);
    case 'switched-off-by-host': {
      const count = memory?.switched_off?.stops.length ?? 0;
      const window = windowText(memory?.switch_off_window_seconds ?? 0);

      return count > 0 && window !== ''
        ? t(`${KEY}.switchedOff`, { count, window })
        : t(`${KEY}.switchedOffPlain`);
    }
    default:
      return t(`${KEY}.other`, { result: stop.result });
  }
};

/** The warning for a module that is over its budget and keeps running because it is set to only warn. */
export const overBudgetWarning = (
  memory: ResourceMemory | undefined,
  t: Translate,
): string | undefined =>
  memory?.on_over_memory === 'warn' && memory.over_budget
    ? t('app.settings.extensions.memory.overBudget')
    : undefined;
