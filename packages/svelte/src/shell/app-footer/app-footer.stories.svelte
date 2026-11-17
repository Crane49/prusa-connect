<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import AppFooter from './app-footer.svelte';

  const { Story } = defineMeta({
    title: 'Shell/AppFooter',
    component: AppFooter,
  });
</script>

<Story name="Default" args={{ host: 'printer.local', appVersion: '0.1.0' }} />

<Story name="Host Only" args={{ host: 'printer.local' }} />
