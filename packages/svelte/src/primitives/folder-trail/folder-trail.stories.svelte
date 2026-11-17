<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import FolderTrail from './folder-trail.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/Folder Trail',
    component: FolderTrail,
    args: { label: 'Folder path', onOpen: () => undefined },
  });
</script>

<Story name="At The Root" args={{ crumbs: [{ label: 'Storage', path: '' }] }} />

<Story
  name="Two Levels Deep"
  args={{
    crumbs: [
      { label: 'Storage', path: '' },
      { label: 'Benchy', path: 'Benchy' },
      { label: 'Plates', path: 'Benchy/Plates' },
    ],
  }}
/>

<Story
  name="Long Names"
  args={{
    crumbs: [
      { label: 'Storage', path: '' },
      { label: 'Customer prototypes 2026', path: 'a' },
      { label: 'Housing revision C, final', path: 'a/b' },
      { label: 'Plates for the Friday batch', path: 'a/b/c' },
    ],
  }}
/>
