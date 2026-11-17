<!--
  The page of one file on the server's own storage, over the story core.
  States: a sliced file the core described, a file the core could not
  describe (no print details), a file in a folder, loading, failed, refused.
  A file the listing does not hold stays on the loading state, as the screen
  draws it.
-->
<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { json } from '../../../src/test/stub';
  import { failed, pending, refused } from '../../../test/story/api';
  import AppAt from '../../../test/story/app-at.svelte';
  import { storageFileHref } from '../routes';

  const { Story } = defineMeta({ title: 'Screens/Storage/File detail' });

  const file = storageFileHref('local', 'bracket.bgcode');
  const inFolder = storageFileHref(
    'local',
    'q3_rush_jobs/urgent_bracket.gcode',
  );

  const described = {
    name: 'bracket.bgcode',
    path: 'bracket.bgcode',
    folder: false,
    size: 2_148_576,
    modified: '2026-09-19T11:02:00Z',
    print: {
      estimated_seconds: 7260,
      filament_mm: 18_420,
      filament_g: 55.2,
      layer_height_mm: 0.2,
      nozzle_mm: 0.4,
      material: 'PLA',
      printer_model: 'MK4S',
    },
  };
</script>

<Story name="Sliced file">
  <AppAt
    path={file}
    overrides={{
      'GET /api/v1/storages/local/stat?path=bracket.bgcode': () =>
        json(200, described),
    }}
  />
</Story>
<Story name="Without details"><AppAt path={file} /></Story>
<Story name="In a folder"><AppAt path={inFolder} /></Story>
<Story name="Loading">
  <AppAt
    path={file}
    overrides={{ 'GET /api/v1/storages/local/files': pending }}
  />
</Story>
<Story name="Failed">
  <AppAt
    path={file}
    overrides={{ 'GET /api/v1/storages/local/files': failed }}
  />
</Story>
<Story name="Refused">
  <AppAt
    path={file}
    overrides={{ 'GET /api/v1/storages/local/files': refused }}
  />
</Story>
