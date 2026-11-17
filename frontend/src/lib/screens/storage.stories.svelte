<!--
  The storage screen over the story core.
  States: the bare address (the folder last left), a storage root, a folder,
  an empty storage, loading, failed (the storage list and the folder listing),
  refused, a server in a Fractal network, and one whose shares could not be
  read. The writable-storage actions are
  drawn as the core describes them.
-->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { failed, pending, refused } from '../../../test/story/api';
  import AppAt from '../../../test/story/app-at.svelte';
  import { operationTable } from 'fractal-client';

  import { json, problem } from '../../test/stub';
  import { storageHref } from '../routes';

  const { Story } = defineMeta({ title: 'Screens/Storage' });

  const SHARES = operationTable['list-fractal-shares'].path;
  const STORES = operationTable['list-fractal-stores'].path;

  // brno, below hq, shares q3_rush_jobs and sees a folder hq shares.
  const fractal = {
    'GET /api/v1/resources': json(200, {
      items: [
        {
          config: {},
          enabled: true,
          generation: 1,
          kind: 'link',
          name: 'fractal',
          namespace: 'com.github.aux.prusa3d.fractal-link',
          required: false,
          runtime: 'process',
          source: 'file',
          status: { observed_generation: 1, serving: true, state: 'ready' },
          type: 'fractal',
          uid: 'uid-fractal',
          version: '1.0.0',
        },
      ],
    }),
    [`GET ${SHARES}`]: json(200, {
      items: [{ storage: 'local', path: 'q3_rush_jobs' }],
    }),
    [`GET ${STORES}`]: json(200, {
      node: 'hq/brno',
      items: [
        {
          node: 'hq',
          interface: 'ContentStore1',
          instance: 'local',
          status: 'OK',
          updated_at: 1_793_700_000_000,
          props: { rootHash: 'sha256:aa' },
        },
      ],
      total: 1,
      truncated: false,
    }),
    [`GET ${STORES}/hq/local/sha256:aa`]: json(200, {
      node: 'hq',
      instance: 'local',
      identity: 'sha256:aa',
      entries: [
        { name: 'fixtures', kind: 'directory', size: 0, identity: 'sha256:bb' },
      ],
    }),
  };
</script>

<Story name="Root"><AppAt path={storageHref('local')} /></Story>
<Story name="Bare address"><AppAt path={storageHref()} /></Story>
<Story name="Folder">
  <AppAt path={storageHref('local', 'q3_rush_jobs')} />
</Story>
<Story name="Empty"><AppAt path={storageHref('scratch')} /></Story>
<Story name="Loading">
  <AppAt
    path={storageHref('local')}
    overrides={{ 'GET /api/v1/storages': pending }}
  />
</Story>
<Story name="Listing loading">
  <AppAt
    path={storageHref('local')}
    overrides={{ 'GET /api/v1/storages/local/files': pending }}
  />
</Story>
<Story name="Failed">
  <AppAt
    path={storageHref('local')}
    overrides={{ 'GET /api/v1/storages': failed }}
  />
</Story>
<Story name="Listing failed">
  <AppAt
    path={storageHref('local')}
    overrides={{ 'GET /api/v1/storages/local/files': failed }}
  />
</Story>
<Story name="Refused">
  <AppAt
    path={storageHref('local')}
    overrides={{ 'GET /api/v1/storages': refused }}
  />
</Story>
<Story name="Fractal server">
  <AppAt path={storageHref('local')} overrides={fractal} />
</Story>
<Story name="Fractal shares failed">
  <AppAt
    path={storageHref('local')}
    overrides={{
      ...fractal,
      [`GET ${SHARES}`]: problem(503, 'core.internal/RESOURCE_NOT_READY'),
    }}
  />
</Story>
