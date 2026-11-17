<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import CopyField from './copy-field.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/CopyField',
    component: CopyField,
    args: { label: 'API token', value: 'a1b2c3d4e5f6' },
  });
</script>

<Story name="Default" />

<Story name="Copy Succeeds And Is Announced" tags={['!dev', '!autodocs']} />

<Story name="Copy Failure Is Surfaced" tags={['!dev', '!autodocs']} />

<Story name="Overlapping Clicks Do Not Race" tags={['!dev', '!autodocs']} />
