<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import CopyButton from './copy-button.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/Copy Button',
    component: CopyButton,
    args: { value: 'SN-MK4S-0007-3312', label: 'Copy serial number' },
  });
</script>

<Story name="Copy A Fact" />

<!-- The outcome lands on the button, not in a toast, and in words for a reader
     who cannot see the glyph change. -->
<Story name="It Says So In Place" tags={['!dev', '!autodocs']} />
