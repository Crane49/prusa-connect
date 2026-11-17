import type { ResourceInstance } from '@prusa/connect-ui/data';
import { hiddenEntries } from '@prusa/connect-ui/slots';

/**
 * What the installed modules draw, as one comparable value. A module draws
 * while the server publishes its `ui`, which it does only for an enabled
 * instance whose bundle the plane holds.
 */
export const uiSignature = (items: readonly ResourceInstance[]): string =>
  items
    .filter((item) => item.ui !== undefined)
    .map((item) => `${item.namespace ?? ''}@${item.module?.version ?? ''}`)
    .sort()
    .join('\n');

export interface RefreshableModules {
  readonly items: readonly ResourceInstance[];
  readonly error?: Error | undefined;
  load(options?: { fresh?: boolean }): Promise<void>;
}

export interface ReloadableLoader {
  reload(): Promise<void>;
}

/** The half of the slot registry that takes a module's hidden entries. */
export interface HidingRegistry {
  hide(namespace: string, hidden: readonly string[]): void;
}

/**
 * Re-reads the installed modules, and reloads the slots only when what they
 * draw changed: a reload aborts every bundle, so a retry of a module that
 * keeps drawing must not cause one. A failed read leaves the list empty, which
 * says nothing about what draws, so it reloads nothing.
 *
 * The entries a module or the operator hides change without a reload: the
 * registry is handed each module's list again, so a file action or a section
 * a module hides goes as its page leaves the menu (ADR-20261021-6h2).
 */
export const moduleRefresh =
  ({
    modules,
    loader,
    registry,
  }: {
    modules: RefreshableModules;
    loader: ReloadableLoader;
    registry?: HidingRegistry;
  }) =>
  async (): Promise<void> => {
    const before = uiSignature(modules.items);
    await modules.load({ fresh: true });
    if (modules.error !== undefined) return;
    if (uiSignature(modules.items) !== before) {
      await loader.reload();

      return;
    }
    for (const item of modules.items) {
      if (item.ui === undefined || item.namespace === undefined) continue;
      registry?.hide(item.namespace, hiddenEntries(item));
    }
  };

/** Runs `run` once, `ms` after the last of a burst of calls; `cancel` drops a pending run. */
export const debounced = (
  run: () => void,
  ms: number,
): (() => void) & { cancel(): void } => {
  let timer: ReturnType<typeof setTimeout> | undefined;
  return Object.assign(
    () => {
      clearTimeout(timer);
      timer = setTimeout(run, ms);
    },
    {
      cancel: () => {
        clearTimeout(timer);
      },
    },
  );
};
