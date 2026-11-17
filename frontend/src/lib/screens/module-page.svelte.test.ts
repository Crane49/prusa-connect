import { describe, expect, it } from 'vitest';
import { page } from 'vitest/browser';
import { render } from 'vitest-browser-svelte';

import { Fleet, Modules } from '@prusa/connect-ui/data';
import {
  createModuleLoader,
  type ModuleContributes,
  SlotRegistry,
} from '@prusa/connect-ui/slots';

import { appContext } from '../../test/app-context';
import PlainPage from '../../test/plain-page.svelte';
import { json, stubClient } from '../../test/stub';
import WithSlots from '../../test/with-slots.svelte';
import ModulePageScreen from './module-page.svelte';

const NAMESPACE = 'com.github.aux.acme.spools';

const contributes: ModuleContributes = {
  pages: [{ path: 'spools', title: 'Spools', hidden: false }],
};

/** A loader that has read an empty listing, so its run is over. */
const ranLoader = async (registry: SlotRegistry) => {
  const { client } = stubClient({
    'GET /api/v1/resources': json(200, { items: [] }),
    'GET /api/v1/principal': json(200, { principal: 'user:demo' }),
  });
  const loader = createModuleLoader({
    client,
    registry,
    fleet: new Fleet(client),
  });
  await loader.activate();

  return loader;
};

const openAt = (
  registry: SlotRegistry,
  namespace: string,
  path: string,
  loader?: Awaited<ReturnType<typeof ranLoader>>,
) =>
  render(WithSlots, {
    props: {
      registry,
      loader,
      screen: ModulePageScreen,
      ctx: appContext({}, { params: { namespace, path } }),
    },
  });

describe('a module page', () => {
  it('says the bundle is on its way while the loader has not run', async () => {
    await openAt(new SlotRegistry(), NAMESPACE, 'spools');

    await expect
      .element(page.getByText("Opening this extension's page"))
      .toBeInTheDocument();
  });

  // Two empty answers, two sentences: "it is coming" and "there is none".
  it('says the server has no such page once the loader has run', async () => {
    const registry = new SlotRegistry();
    const loader = await ranLoader(registry);

    await openAt(registry, NAMESPACE, 'spools', loader);

    await expect
      .element(
        page.getByText('This server has no extension page at this address.'),
      )
      .toBeInTheDocument();
    await expect
      .element(page.getByRole('link', { name: 'All extensions' }))
      .toHaveAttribute('href', '/settings/extensions');
  });

  // ADR-20261019-h76 rule 11: the module is installed and the operator's key keeps
  // it off, which is neither "it is coming" nor "there is none".
  it('says the operator switched the module off', async () => {
    const registry = new SlotRegistry();
    const loader = await ranLoader(registry);
    const { client } = stubClient({
      'GET /api/v1/resources': json(200, {
        items: [
          {
            kind: 'asset',
            name: 'spools',
            namespace: NAMESPACE,
            uid: 'uid-spools',
            enabled: true,
            source: 'overlay',
            status: {
              state: 'disabled',
              conditions: [
                {
                  type: 'Started',
                  status: 'False',
                  reason: 'ModulesDisabled',
                  last_transition_time: '2026-10-19T00:00:00Z',
                  observed_generation: 1,
                },
              ],
            },
          },
        ],
      }),
    });
    const modules = new Modules(client);
    await modules.load();

    await render(WithSlots, {
      props: {
        registry,
        loader,
        screen: ModulePageScreen,
        ctx: appContext(
          {},
          { modules, params: { namespace: NAMESPACE, path: 'spools' } },
        ),
      },
    });

    await expect
      .element(
        page.getByText(/does not run, because core.modules_enabled is false/u),
      )
      .toBeInTheDocument();
    expect(document.body.textContent).not.toContain('no extension page');
  });

  it('promises nothing for a module its own flag keeps off', async () => {
    const registry = new SlotRegistry();
    const loader = await ranLoader(registry);
    const { client } = stubClient({
      'GET /api/v1/resources': json(200, {
        items: [
          {
            kind: 'asset',
            name: 'spools',
            namespace: NAMESPACE,
            uid: 'uid-spools',
            enabled: false,
            source: 'overlay',
            status: {
              state: 'disabled',
              conditions: [
                {
                  type: 'Started',
                  status: 'False',
                  reason: 'ModulesDisabled',
                  last_transition_time: '2026-10-19T00:00:00Z',
                  observed_generation: 1,
                },
              ],
            },
          },
        ],
      }),
    });
    const modules = new Modules(client);
    await modules.load();

    await render(WithSlots, {
      props: {
        registry,
        loader,
        screen: ModulePageScreen,
        ctx: appContext(
          {},
          { modules, params: { namespace: NAMESPACE, path: 'spools' } },
        ),
      },
    });

    await expect
      .element(
        page.getByText('This server has no extension page at this address.'),
      )
      .toBeInTheDocument();
    expect(document.body.textContent).not.toContain('open again');
  });

  it('draws the component the module registered for the route', async () => {
    const registry = new SlotRegistry();
    registry.declare(NAMESPACE, contributes);
    registry.registerPage(NAMESPACE, 'spools', PlainPage);

    await openAt(registry, NAMESPACE, 'spools');

    await expect.element(page.getByTestId('plain-page')).toBeInTheDocument();
  });

  // A module reads the printers through the host and has no way to ask for
  // them itself, so the screen that draws its page reads them.
  it('reads the printers a module page may draw', async () => {
    const ctx = appContext(
      { 'GET /api/v1/devices': json(200, { items: [] }) },
      { params: { namespace: NAMESPACE, path: 'spools' } },
    );

    await render(WithSlots, {
      props: { registry: new SlotRegistry(), screen: ModulePageScreen, ctx },
    });

    await expect.poll(() => ctx.fleet.loaded).toBe(true);
  });

  it('draws nothing of another module at the same path', async () => {
    const registry = new SlotRegistry();
    registry.declare(NAMESPACE, contributes);
    registry.registerPage(NAMESPACE, 'spools', PlainPage);

    await openAt(registry, 'com.github.aux.acme.other', 'spools');

    await expect
      .element(page.getByTestId('plain-page'))
      .not.toBeInTheDocument();
  });
});
