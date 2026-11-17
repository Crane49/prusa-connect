<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import SaveToStorage from './save-to-storage.svelte';

  const { Story } = defineMeta({
    title: 'Parts/Screen components/Printer Detail/Save To Storage',
    component: SaveToStorage,
  });

  const quiet = {
    onKeepChange: () => undefined,
    onStorageChange: () => undefined,
    onFolderChange: () => undefined,
    onPickerWalk: () => undefined,
  };

  const one = [{ name: 'local', label: 'Local' }];
  const two = [
    { name: 'local', label: 'Local' },
    { name: 'nas', label: 'Workshop NAS' },
  ];
</script>

<!-- The default: the file is sent and not kept. -->
<Story
  name="Send And Forget"
  args={{
    ...quiet,
    keep: false,
    storages: one,
    storage: 'local',
    folder: '',
    pickerAt: '',
    pickerFolders: [],
  }}
/>

<!-- Kept at the root of the one storage, so no storage is offered. -->
<Story
  name="Kept At The Root"
  args={{
    ...quiet,
    keep: true,
    storages: one,
    storage: 'local',
    folder: '',
    pickerAt: '',
    pickerFolders: [],
  }}
/>

<!-- Two storages to choose between, and a folder deep in the second. -->
<Story
  name="Kept In A Folder Of Another Storage"
  args={{
    ...quiet,
    keep: true,
    storages: two,
    storage: 'nas',
    folder: 'prints/brackets',
    pickerAt: '',
    pickerFolders: [],
  }}
/>
