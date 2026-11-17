<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import ColorInput from './color-input.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/Color Input',
    component: ColorInput,
    args: {
      value: '#D32F2F',
      oninput: () => undefined,
      label: 'Primary colour',
    },
  });
</script>

<Story name="Default" />

<Story name="Black" args={{ value: '#000000' }} />
