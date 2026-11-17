import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import type { ReadonlyFleet } from '../domain/fleet';
import { fakeLoaderClient } from '../test/fakes';
import { holdHost } from '../test/held-host';
import ImportingPage from '../test/importing-page.svelte';
import PickerComponent from '../test/picker-component.svelte';
import { defineModule, type ModuleSetup } from './host';
import type { ModuleImport } from './import-module';
import { createModuleLoader } from './loader.svelte';
import { SlotRegistry } from './registry.svelte';
import type { ModuleUI } from './types';

// One module's page draws another module's component, through the loader,
// the registry and the host object a real bundle receives
// (ADR-20261027-wvj rules 19 to 24).

const unreadFleet: ReadonlyFleet = {
  devices: [],
  loading: false,
  loaded: false,
  error: undefined,
  device: () => undefined,
};

const uiOf = (namespace: string, extra: Partial<ModuleUI> = {}): ModuleUI => ({
  entry_url: `/api/v1/modules/${namespace}/ui/index.js`,
  library: '>=0.1.0 <1.0.0',
  contributes: {
    pages: [{ path: namespace, title: namespace, hidden: false }],
  },
  ...extra,
});

const providerUI = uiOf('spools', { exports: [{ name: 'spool-picker' }] });
const importerUI = uiOf('planner', {
  imports: [{ module: 'spools', name: 'spool-picker' }],
});

const exportsPicker = defineModule((host) => {
  host.register.component('spool-picker', PickerComponent);
});

const importer = defineModule((host) => {
  holdHost(host);
  host.register.page('planner', ImportingPage);
});

const setUp = (
  modules: readonly { namespace: string; ui: ModuleUI }[],
  setups: Record<string, ModuleSetup>,
) => {
  const registry = new SlotRegistry();
  const imported: string[] = [];
  const importModule: ModuleImport = (url) => {
    imported.push(url);
    const found = Object.entries(setups).find(([namespace]) =>
      url.includes(`/modules/${namespace}/`),
    );

    return Promise.resolve(found ? { default: found[1] } : {});
  };
  const loader = createModuleLoader({
    client: fakeLoaderClient(modules),
    registry,
    fleet: unreadFleet,
    version: '0.1.0',
    importModule,
    createStore: () => ({
      get: () => undefined,
      set: () => undefined,
      delete: () => undefined,
      keys: () => [],
    }),
  });

  return { loader, registry, imported };
};

describe('a module draws another module component', () => {
  it('renders the component with props and hands the pick back through a callback', async () => {
    const { loader, imported } = setUp(
      [
        { namespace: 'planner', ui: importerUI },
        { namespace: 'spools', ui: providerUI },
      ],
      { planner: importer, spools: exportsPicker },
    );

    // Only the importing module is loaded up front.
    await loader.activateFor(['planner']);
    expect(imported).toEqual(['/api/v1/modules/planner/ui/index.js']);

    await render(ImportingPage);

    await page.getByRole('button', { name: 'PETG' }).click();

    await expect.element(page.getByTestId('picked')).toHaveTextContent('PETG');
    expect(imported).toContain('/api/v1/modules/spools/ui/index.js');
  });

  // ADR-20261029-zzg: a module's styles reach only markup under its own
  // name, so a component drawn in another module's page stands under the
  // name of the module that exported it.
  it('draws the component under the name of the module that exported it', async () => {
    const { loader } = setUp(
      [
        { namespace: 'planner', ui: importerUI },
        { namespace: 'spools', ui: providerUI },
      ],
      { planner: importer, spools: exportsPicker },
    );
    await loader.activateFor(['planner']);

    await render(ImportingPage);

    await expect.element(page.getByTestId('picker')).toBeInTheDocument();
    expect(
      page.getByTestId('picker').element().closest('[data-module]'),
    ).toHaveAttribute('data-module', 'spools');
  });

  // The prelude a bundle writes once it knows its namespace. One module's
  // rule reaches its own markup and stops at another module's inside it.
  it('keeps one module rule off the markup of another module inside it', () => {
    const scoped = (namespace: string, order: number) => {
      const self = `[data-module=${JSON.stringify(namespace)}]`;
      const style = document.createElement('style');
      style.textContent = `@scope (${self}) to ([data-module]:not(${self})){@layer utilities{p{order:${String(order)}}}}`;
      document.head.append(style);

      return style;
    };
    const sheets = [scoped('planner', 2), scoped('spools', 3)];
    const outer = document.createElement('div');
    outer.dataset.module = 'planner';
    outer.innerHTML =
      '<p id="own"></p><div data-module="spools"><p id="inner"></p></div>';
    document.body.append(outer);

    expect(getComputedStyle(outer.querySelector('#own')!).order).toBe('2');
    expect(getComputedStyle(outer.querySelector('#inner')!).order).toBe('3');

    outer.remove();
    for (const style of sheets) style.remove();
  });

  it('falls back when the import is not declared', async () => {
    const { loader } = setUp(
      [
        { namespace: 'planner', ui: uiOf('planner') },
        { namespace: 'spools', ui: providerUI },
      ],
      { planner: importer, spools: exportsPicker },
    );
    await loader.activateFor(['planner']);

    await render(ImportingPage);

    await expect
      .element(page.getByTestId('fallback'))
      .toHaveTextContent('undeclared');
  });

  it('falls back when the exporting module is not installed', async () => {
    const { loader } = setUp([{ namespace: 'planner', ui: importerUI }], {
      planner: importer,
    });
    await loader.activateFor(['planner']);

    await render(ImportingPage);

    await expect
      .element(page.getByTestId('fallback'))
      .toHaveTextContent('absent');
  });

  it('falls back when the module exports no such component', async () => {
    const { loader } = setUp(
      [
        { namespace: 'planner', ui: importerUI },
        { namespace: 'spools', ui: uiOf('spools') },
      ],
      { planner: importer, spools: exportsPicker },
    );
    await loader.activateFor(['planner']);

    await render(ImportingPage);

    await expect
      .element(page.getByTestId('fallback'))
      .toHaveTextContent('not-exported');
  });

  it('falls back when the bundle registers nothing under the name', async () => {
    const { loader } = setUp(
      [
        { namespace: 'planner', ui: importerUI },
        { namespace: 'spools', ui: providerUI },
      ],
      { planner: importer, spools: defineModule(() => undefined) },
    );
    await loader.activateFor(['planner']);

    await render(ImportingPage);

    await expect
      .element(page.getByTestId('fallback'))
      .toHaveTextContent('unregistered');
  });

  it('drops a module that registers a component its manifest does not export', async () => {
    const { loader, registry } = setUp(
      [
        { namespace: 'planner', ui: importerUI },
        { namespace: 'spools', ui: uiOf('spools') },
      ],
      { planner: importer, spools: exportsPicker },
    );

    await loader.activate();

    expect(registry.refusals()).toEqual([
      {
        namespace: 'spools',
        reason: 'undeclared',
        slot: 'component',
        name: 'spool-picker',
      },
    ]);
    expect(registry.component('spools', 'spool-picker')).toBeUndefined();
  });

  it('refuses a load made while the importing module is still being set up', async () => {
    let refused = '';
    const { loader } = setUp(
      [
        { namespace: 'planner', ui: importerUI },
        { namespace: 'spools', ui: providerUI },
      ],
      {
        planner: defineModule(async (host) => {
          await host.components
            .load('spools', 'spool-picker')
            .catch((thrown: unknown) => {
              refused =
                thrown instanceof Error ? thrown.message : String(thrown);
            });
        }),
        spools: exportsPicker,
      },
    );

    await loader.activateFor(['planner']);

    expect(refused).toContain('setup');
  });

  it('answers a component already registered even while the importing module sets up', async () => {
    let answered = false;
    const { loader } = setUp(
      [
        { namespace: 'spools', ui: providerUI },
        { namespace: 'planner', ui: importerUI },
      ],
      {
        spools: exportsPicker,
        planner: defineModule(async (host) => {
          await host.components.load('spools', 'spool-picker');
          answered = true;
        }),
      },
    );

    await loader.activate();

    expect(answered).toBe(true);
  });
});
