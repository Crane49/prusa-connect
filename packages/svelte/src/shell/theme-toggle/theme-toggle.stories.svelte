<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import ThemeToggle from './theme-toggle.svelte';

  const { Story } = defineMeta({
    title: 'Shell/ThemeToggle',
    component: ThemeToggle,
  });
</script>

<Story name="Default" />
