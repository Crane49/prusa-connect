<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { instanceOf, offerOf } from '../../../test/extension-fixtures';
  import { failed } from '../../../test/story/api';
  import AppAt from '../../../test/story/app-at.svelte';
  import fixtures from '../../../test/ux/fixtures/api.json';
  import { json } from '../../test/stub';
  import { PATHS } from '../routes';

  const { Story } = defineMeta({
    title: 'Screens/Settings/Extensions/Extension detail',
  });

  const NAMESPACE = 'com.github.prusa3d.spools';
  const at = (tab?: string): string =>
    (tab === undefined ? PATHS.extensionDetail : PATHS.extensionDetailTab)
      .replace(':namespace', NAMESPACE)
      .replace(':tab', tab ?? '');

  const ui = JSON.parse(fixtures['GET /api/v1/ui'].body) as Record<
    string,
    unknown
  >;
  const resources = JSON.parse(fixtures['GET /api/v1/resources'].body) as {
    items: unknown[];
  };

  const spools = instanceOf({
    name: 'spools',
    namespace: NAMESPACE,
    module: {
      title: 'Spools',
      version: '1.0.0',
      summary: 'Tracks the filament spools on each printer.',
      asked: {
        permissions: ['observe', 'operate'],
        listen: [
          {
            name: 'web',
            network: 'tcp',
            port: 8080,
            why: 'serves its own page',
          },
        ],
      },
      options: [
        {
          key: 'low_grams',
          type: 'int',
          default: '100',
          summary: 'Below this weight a spool counts as low.',
        },
        {
          key: 'warn_by_mail',
          type: 'bool',
          default: 'false',
          summary: 'Whether a low spool sends a mail.',
        },
      ],
    },
  });
  const offer = offerOf({
    module: 'github.com/prusa3d/spools',
    name: 'spools',
    title: 'Spools',
    namespace: NAMESPACE,
    installed: true,
    installed_version: '1.0.0',
    instance: 'driver:spools',
  });

  // Spools as it requires a reader, suggests a parts list, and as a print planner
  // planner requires it (ADR-20261027-e6k).
  const dependent = instanceOf({
    ...spools,
    module: {
      ...spools.module,
      requires: [
        {
          module: 'com.github.prusa3d.reader',
          version: '^1.2',
          met: true,
          installed: '1.4.0',
          instance: 'sink:reader',
          state: 'ready',
        },
      ],
      suggests: [
        {
          module: 'com.github.prusa3d.parts',
          reason: 'Shows the parts of a plan',
        },
      ],
      required_by: [
        {
          instance: 'sink:planner',
          module: 'com.github.prusa3d.planner',
          version: '^1.0',
          enabled: true,
        },
      ],
    },
  });

  const memoryOf = (over: Record<string, unknown>) =>
    instanceOf({
      ...spools,
      on_over_memory: 'warn',
      status: {
        ...spools.status,
        memory: {
          budget_bytes: 256 * 1_048_576,
          budget_source: 'author',
          held_until_restart: false,
          near_budget: false,
          on_over_memory: 'warn',
          over_budget: false,
          failures_in_window: 0,
          switch_off_failures: 3,
          switch_off_window_seconds: 3600,
          switch_off_source: 'default',
          used_bytes: 96 * 1_048_576,
          ...over,
        },
      },
    });
  const servedMemory = (instance: unknown, pool: () => Response) => ({
    ...served,
    'GET /api/v1/resources': () =>
      json(200, { items: [...resources.items, instance] }),
    'GET /api/v1/modules/memory': pool,
  });

  const minutesAgo = (minutes: number): string =>
    new Date(Date.now() - minutes * 60_000).toISOString();
  // A reading every two minutes, one of them unread, and one stop.
  const history = {
    items: Array.from({ length: 30 }, (_, at) => ({
      at: minutesAgo(60 - at * 2),
      ...(at === 12 ? {} : { value: (60 + ((at * 7) % 40)) * 1_048_576 }),
    })),
    stops: [
      {
        at: minutesAgo(20),
        result: 'over-memory',
        used_bytes: 140 * 1_048_576,
        budget_bytes: 128 * 1_048_576,
      },
    ],
  };
  const servedHistory = (
    over: Record<string, () => Response>,
  ): Record<string, () => Response> => ({
    ...servedMemory(memoryOf({}), () =>
      json(403, { title: 'Forbidden', status: 403 }),
    ),
    'GET /api/v1/resources/sink/spools/memory': () => json(200, history),
    'GET /api/v1/resources/sink/spools/log': () =>
      json(200, {
        lines: [
          { at: minutesAgo(5), text: 'listening on :8080' },
          { at: minutesAgo(4), text: 'spool 12 is low' },
        ],
      }),
    ...over,
  });

  const served = {
    'GET /api/v1/resources': () =>
      json(200, { items: [...resources.items, spools] }),
    'GET /api/v1/modules/index': () => json(200, { items: [offer] }),
  };

  // Spools as it takes a file on a staged route, with the limit the operator
  // raised above what it asked for (ADR-20261120-xys rule 5).
  const upload = { method: 'PUT', path: '/files/{name}', max_mib: 1024 };
  const servedUploads = {
    ...served,
    'GET /api/v1/resources': () =>
      json(200, {
        items: [
          ...resources.items,
          instanceOf({
            ...spools,
            staged_routes: [{ ...upload, max_mib: 2048 }],
            module: {
              ...spools.module,
              asked: { ...spools.module?.asked, staged_routes: [upload] },
            },
          }),
        ],
      }),
  };
</script>

<!--
  States the screen branches on: the first tab, the Ports, Uploads, Memory, Dependencies and
  Permissions tabs
  through PATHS.extensionDetailTab, an extension that is gone, modules
  switched off, and the installed listing failed, where the page keeps its
  loading shape because nothing on it says the read failed.
  Omitted Loading: the app draws a blank page until GET /api/v1/resources answers, so the page's loading shape shows only after that read failed, which the Failed story shows.
  Omitted Refused: GET /api/v1/resources never refuses a signed-in person, it lists only what they may see, and a refused marketplace index is not drawn here.
  The Visibility tab and the
  panes an extension adds to Settings need its browser bundle loaded and are
  skipped. A switch, a saved setting and a removal write and are skipped.
-->

<Story name="Settings Tab"><AppAt path={at()} overrides={served} /></Story>

<Story name="Ports Tab">
  <AppAt path={at('ports')} overrides={served} />
</Story>

<Story name="Uploads Tab">
  <AppAt path={at('uploads')} overrides={servedUploads} />
</Story>

<Story name="Memory Tab">
  <AppAt
    path={at('memory')}
    overrides={servedMemory(memoryOf({}), () =>
      json(200, {
        committed_bytes: 1024 * 1_048_576,
        pool_bytes: 3072 * 1_048_576,
        source: 'detected',
      }),
    )}
  />
</Story>

<Story name="Memory Tab Running Over Budget">
  <AppAt
    path={at('memory')}
    overrides={servedMemory(
      memoryOf({ over_budget: true, used_bytes: 300 * 1_048_576 }),
      () => json(403, { title: 'Forbidden', status: 403 }),
    )}
  />
</Story>

<Story name="Memory Tab Last Hour">
  <AppAt path={at('memory')} overrides={servedHistory({})} />
</Story>

<Story name="Memory Tab No Readings">
  <AppAt
    path={at('memory')}
    overrides={servedHistory({
      'GET /api/v1/resources/sink/spools/memory': () =>
        json(200, { items: [], stops: [] }),
    })}
  />
</Story>

<Story name="Memory Tab History Failed">
  <AppAt
    path={at('memory')}
    overrides={servedHistory({
      'GET /api/v1/resources/sink/spools/memory': failed,
    })}
  />
</Story>

<Story name="Log Tab Failed">
  <AppAt
    path={at('log')}
    overrides={servedHistory({
      'GET /api/v1/resources/sink/spools/log': failed,
    })}
  />
</Story>

<Story
  name="Restart Refused"
  play={async ({ canvasElement }) => {
    const restart = (): HTMLElement | undefined =>
      [...canvasElement.querySelectorAll('button')].find(
        (one) => one.textContent.trim() === 'Restart',
      );
    for (let tries = 0; tries < 50 && restart() === undefined; tries += 1) {
      await new Promise((done) => setTimeout(done, 100));
    }
    restart()?.click();
  }}
>
  <AppAt
    path={at('memory')}
    overrides={servedHistory({
      'POST /api/v1/resources/sink/spools/restart': () =>
        json(403, { title: 'Forbidden', status: 403 }),
    })}
  />
</Story>

<Story name="Log Tab">
  <AppAt path={at('log')} overrides={servedHistory({})} />
</Story>

<Story name="Log Tab Refused">
  <AppAt
    path={at('log')}
    overrides={servedHistory({
      'GET /api/v1/resources/sink/spools/log': () =>
        json(403, { title: 'Forbidden', status: 403 }),
    })}
  />
</Story>

<Story name="Log Tab Empty">
  <AppAt
    path={at('log')}
    overrides={servedHistory({
      'GET /api/v1/resources/sink/spools/log': () => json(200, { lines: [] }),
    })}
  />
</Story>

<Story name="Permissions Tab">
  <AppAt path={at('permissions')} overrides={served} />
</Story>

<Story name="Dependencies Tab">
  <AppAt
    path={at('dependencies')}
    overrides={{
      ...served,
      'GET /api/v1/resources': () =>
        json(200, { items: [...resources.items, dependent] }),
      [`GET /api/v1/modules/why/${NAMESPACE}`]: () =>
        json(200, {
          items: [
            {
              by: 'com.github.prusa3d.planner',
              constraint: '^1.0',
              text: `com.github.prusa3d.planner 1.0.0 requires ${NAMESPACE} ^1.0`,
            },
          ],
        }),
      'GET /api/v1/modules/outdated': () =>
        json(200, {
          items: [
            {
              namespace: NAMESPACE,
              installed: '1.0.0',
              allowed: '1.1.0',
              latest: '1.1.0',
              status: 'update-possible',
            },
          ],
        }),
    }}
  />
</Story>

<Story name="Modules Switched Off">
  <AppAt
    path={at('ports')}
    overrides={{
      ...served,
      'GET /api/v1/ui': () => json(200, { ...ui, modules: false }),
    }}
  />
</Story>

<Story name="Gone">
  <AppAt path={at()} />
</Story>

<Story name="Failed">
  <AppAt path={at()} overrides={{ 'GET /api/v1/resources': failed }} />
</Story>
