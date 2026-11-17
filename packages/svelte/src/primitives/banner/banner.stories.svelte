<script module lang="ts">
  import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import Button from '../button/button.svelte';
  import Banner from './banner.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/Banner',
    component: Banner,
    args: { tone: 'warning', icon: TriangleAlertIcon, children: message },
  });
</script>

{#snippet message()}
  The server is running with every module off.
{/snippet}

{#snippet restart()}
  <Button variant="secondary">Restart normally</Button>
{/snippet}

<Story name="Warning" />

<Story name="With Actions" args={{ actions: restart }} />

<Story name="Without Icon" args={{ icon: undefined }} />
