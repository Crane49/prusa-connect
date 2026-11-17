// A module's memory budget as the person reads it before enabling: how much,
// who chose it, and whether the enabled modules together outgrow the pool.
import type { components } from '@prusa/connect';
import { formatNumber, type Translate } from '@prusa/connect-ui/i18n';
import type { ProgressTone } from '@prusa/connect-ui/primitives';

import type { ExtensionRow } from './rows';
import { type ResourceMemory, windowText } from './stops';

export type ModuleMemoryPool = components['schemas']['ModuleMemoryPool'];
export type BudgetSource = 'default' | 'author' | 'operator';

const MIB = 1024 * 1024;

export const MIN_MEMORY_MIB = 16;
export const MAX_MEMORY_MIB = 8192;

/** The MiB a person typed as a budget, or undefined when it is not a whole number in range. */
export const budgetOfText = (text: string): number | undefined => {
  if (!/^\d+$/.test(text.trim())) return undefined;
  const mib = Number(text);

  return mib >= MIN_MEMORY_MIB && mib <= MAX_MEMORY_MIB ? mib : undefined;
};

/** The budget the server gives a module whose author recommends none. */
export const DEFAULT_MEMORY_MIB = 128;

export interface MemoryEstimate {
  readonly mib: number;
  readonly source: BudgetSource;
  /** Enabled modules together would ask for more than the pool. False when the pool is unknown. */
  readonly overPool: boolean;
  /** What enabled modules would ask for in all, in MiB, once this one is enabled. */
  readonly committedMiB: number;
  /** The pool in MiB, absent when the server cannot learn it. */
  readonly poolMiB?: number;
}

/** The budget a release brings: the author's recommendation, where 0 or none means the default. */
export const offerBudget = (
  memoryMib: number | undefined,
): { budgetBytes: number; source: BudgetSource } =>
  memoryMib !== undefined && memoryMib > 0
    ? { budgetBytes: memoryMib * MIB, source: 'author' }
    : { budgetBytes: DEFAULT_MEMORY_MIB * MIB, source: 'default' };

/**
 * What the releases an install adds ask of the pool together. A release that
 * runs no process asks for nothing.
 */
export const plannedBytes = (
  releases: readonly { memory_mib?: number; process: boolean }[],
): number =>
  releases.reduce(
    (sum, one) =>
      sum + (one.process ? offerBudget(one.memory_mib).budgetBytes : 0),
    0,
  );

/**
 * `pool` is undefined when the server did not say (the person may not read
 * it); an absent `pool_bytes` is an unknown pool, never a pool of zero.
 */
export const memoryEstimate = (input: {
  budgetBytes: number;
  source: BudgetSource;
  pool: ModuleMemoryPool | undefined;
  /** What this module already counts in the pool: its budget when it is enabled, else 0. */
  heldBytes: number;
}): MemoryEstimate => {
  const committed =
    (input.pool?.committed_bytes ?? 0) + input.budgetBytes - input.heldBytes;
  const poolBytes = input.pool?.pool_bytes;

  return {
    mib: Math.round(input.budgetBytes / MIB),
    source: input.source,
    overPool: poolBytes !== undefined && committed > poolBytes,
    committedMiB: Math.round(committed / MIB),
    ...(poolBytes === undefined
      ? {}
      : { poolMiB: Math.round(poolBytes / MIB) }),
  };
};

/** The colour of a usage bar: past the budget, close to it, or well within. */
export const usageTone = (
  overBudget: boolean,
  nearBudget: boolean,
): ProgressTone => {
  if (overBudget) return 'destructive';
  if (nearBudget) return 'warning';
  return 'primary';
};

const CELL = 'app.settings.extensions.memory.cell';

/** Bytes as the MiB a budget is set in, with one decimal at most. */
export const mibText = (bytes: number, t: Translate): string =>
  t('app.settings.extensions.memory.mib', {
    mib: formatNumber(bytes / MIB, { maximumFractionDigits: 1 }),
  });

/** What each enabled service module the listing shows promises the pool. */
export const poolBudgetsOf = (
  rows: readonly ExtensionRow[],
): { key: string; title: string; bytes: number }[] =>
  rows.flatMap((row) =>
    row.instance?.enabled === true &&
    row.instance.runtime === 'service' &&
    row.instance.status.memory !== undefined
      ? [
          {
            key: row.key,
            title: row.title,
            bytes: row.instance.status.memory.budget_bytes,
          },
        ]
      : [],
  );

/**
 * What a service module's process uses against its budget. A module that is
 * not running has no use to read, so it names its budget only; a running one
 * with no reading yet is unknown, never 0.
 */
export const usageText = (
  memory: ResourceMemory,
  t: Translate,
  running: boolean,
): string => {
  if (!running) {
    return t('app.settings.extensions.memory.budgetOnly', {
      mib: formatNumber(memory.budget_bytes / MIB, {
        maximumFractionDigits: 1,
      }),
    });
  }

  return memory.used_bytes === undefined
    ? t(`${CELL}.unknown`, { budget: mibText(memory.budget_bytes, t) })
    : t(`${CELL}.used`, {
        used: mibText(memory.used_bytes, t),
        budget: mibText(memory.budget_bytes, t),
      });
};

/** How often the module failed inside the switch-off window in force; absent when it did not. */
export const failuresText = (
  memory: ResourceMemory,
  t: Translate,
): string | undefined =>
  memory.failures_in_window > 0
    ? t(`${CELL}.failures`, {
        count: memory.failures_in_window,
        window: windowText(memory.switch_off_window_seconds),
      })
    : undefined;
