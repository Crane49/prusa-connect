<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import StorageFolderDialog from './storage-folder-dialog.svelte';

  const { Story } = defineMeta({
    title: 'Parts/Screen components/Storage/Folder Dialog',
    component: StorageFolderDialog,
  });

  const quiet = {
    open: true,
    onOpenChange: () => undefined,
    onWalk: () => undefined,
    onConfirm: () => undefined,
    root: 'Local',
  };

  const folders = [
    { name: 'brackets', path: 'prints/brackets' },
    { name: 'calibration', path: 'prints/calibration' },
  ];
</script>

<!-- Where a file sent from a printer page is kept. -->
<Story
  name="Choosing Where A File Is Kept"
  args={{
    ...quiet,
    title: 'Save the file in',
    description:
      'Choose the folder the file is saved in before it goes to the printer.',
    confirmLabel: 'Save here',
    at: 'prints',
    folders,
  }}
/>

<!-- Moving entries: the folder they are already in cannot be taken. -->
<Story
  name="Moving Into The Folder They Are In"
  args={{
    ...quiet,
    title: 'Move 2 items',
    description: 'Choose the folder the selected items move into.',
    confirmLabel: 'Move here',
    current: 'prints',
    at: 'prints',
    folders,
  }}
/>

<!-- A folder with no folders inside it. -->
<Story
  name="A Folder With No Folders"
  args={{
    ...quiet,
    title: 'Save the file in',
    description:
      'Choose the folder the file is saved in before it goes to the printer.',
    confirmLabel: 'Save here',
    at: 'prints/brackets',
    folders: [],
  }}
/>

<!-- The folders could not be listed. -->
<Story
  name="The Folders Could Not Be Read"
  args={{
    ...quiet,
    title: 'Save the file in',
    description:
      'Choose the folder the file is saved in before it goes to the printer.',
    confirmLabel: 'Save here',
    at: '',
    folders: [],
    fault: {
      code: 'core.storage/UNAVAILABLE',
      message: 'the storage could not carry that out',
    },
  }}
/>
