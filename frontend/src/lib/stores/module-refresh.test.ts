import { describe, expect, it, vi } from 'vitest';

import type { ResourceInstance } from '@prusa/connect-ui/data';

import { moduleRefresh, uiSignature } from './module-refresh';

const drawing = (namespace: string, version = '1.0.0'): ResourceInstance =>
  ({
    kind: 'asset',
    name: namespace,
    namespace,
    module: { version },
    ui: { entry: 'ui/index.js' },
  }) as unknown as ResourceInstance;

const silent = (namespace: string): ResourceInstance =>
  ({
    kind: 'asset',
    name: namespace,
    namespace,
    module: { version: '1.0.0' },
  }) as unknown as ResourceInstance;

const modulesReading = (...reads: ResourceInstance[][]) => {
  let items: readonly ResourceInstance[] = [];
  return {
    error: undefined as Error | undefined,
    get items() {
      return items;
    },
    load: vi.fn(() => {
      items = reads.shift() ?? items;
      return Promise.resolve();
    }),
  };
};

describe('the signature of what modules draw', () => {
  it('ignores order and modules that draw nothing', () => {
    expect(uiSignature([drawing('a'), drawing('b'), silent('c')])).toBe(
      uiSignature([drawing('b'), drawing('a')]),
    );
  });

  it('changes with a version', () => {
    expect(uiSignature([drawing('a', '1.0.0')])).not.toBe(
      uiSignature([drawing('a', '1.1.0')]),
    );
  });
});

describe('a module refresh', () => {
  it('reloads the slots when a module starts drawing', async () => {
    const modules = modulesReading([drawing('a')]);
    const loader = { reload: vi.fn(() => Promise.resolve()) };

    await moduleRefresh({ modules, loader })();

    expect(modules.load).toHaveBeenCalledOnce();
    expect(loader.reload).toHaveBeenCalledOnce();
  });

  it('reloads only when what draws changes', async () => {
    const modules = modulesReading(
      [drawing('a')],
      [drawing('a')],
      [silent('a')],
    );
    const loader = { reload: vi.fn(() => Promise.resolve()) };
    const refresh = moduleRefresh({ modules, loader });

    await refresh(); // a starts drawing
    await refresh(); // a retried, still drawing
    await refresh(); // a switched off

    expect(loader.reload).toHaveBeenCalledTimes(2);
  });

  it('keeps the slots when the read failed', async () => {
    const modules = modulesReading([]);
    modules.load.mockImplementation(() => {
      modules.error = new Error('offline');
      return Promise.resolve();
    });
    const loader = { reload: vi.fn(() => Promise.resolve()) };
    const before = [drawing('a')];
    Object.defineProperty(modules, 'items', {
      get: () => (modules.error ? [] : before),
    });

    await moduleRefresh({ modules, loader })();

    expect(loader.reload).not.toHaveBeenCalled();
  });

  it('hands the registry what each module hides without a reload', async () => {
    const hiding = (hidden: string[], operator: string[] = []) =>
      ({
        ...drawing('planner'),
        hidden_contributions: operator,
        ui: { entry: 'ui/index.js', hidden },
      }) as unknown as ResourceInstance;
    const modules = modulesReading(
      [hiding([])],
      [hiding(['pages/customers'], ['file_action/plan'])],
    );
    const loader = { reload: vi.fn(() => Promise.resolve()) };
    const registry = { hide: vi.fn() };
    const refresh = moduleRefresh({ modules, loader, registry });

    await refresh(); // planner starts drawing, which reloads the slots
    await refresh(); // planner hides its customers page

    expect(loader.reload).toHaveBeenCalledOnce();
    expect(registry.hide).toHaveBeenLastCalledWith('planner', [
      'file_action/plan',
      'pages/customers',
    ]);
  });
});
