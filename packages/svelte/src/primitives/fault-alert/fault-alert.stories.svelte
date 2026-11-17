<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import Button from '../button/button.svelte';
  import FaultAlert from './fault-alert.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/Fault Alert',
    component: FaultAlert,
  });
</script>

{#snippet retryActions()}
  <Button size="sm" variant="secondary">Reload</Button>
  <Button size="sm" variant="ghost">Dismiss</Button>
{/snippet}

<!-- The "report this" link appears only where a host has called `setReportHref`; none does here. -->

<Story
  name="Title Only"
  args={{
    fault: {
      code: undefined,
      title: 'The server could not be reached.',
      detail: undefined,
    },
  }}
/>

<Story
  name="With Detail And Action"
  args={{
    fault: {
      code: 'core.resource/HISTORY_QUERY_REFUSED',
      title: 'The history store refused this window.',
      detail: 'The provider keeps only minute-resolution samples.',
      action:
        'Pick a longer window, or the printer may have been switched off.',
    },
  }}
/>

<Story
  name="With Trace Id"
  args={{
    fault: {
      code: 'core.transport/NETWORK',
      title: 'The server could not be reached.',
      detail: 'connection reset by peer',
      traceId: '4bf92f3577b34da6a3ce929d0e0e4736',
    },
  }}
/>

<Story name="No Fault Draws Nothing" args={{ fault: undefined }} />

<Story
  name="With Actions"
  args={{
    fault: {
      code: 'core.resource/CHANGED',
      title: 'The catalogue changed under you.',
      detail: 'Reload to see the current one.',
    },
    actions: retryActions,
  }}
/>
