<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import FleetIssueSummary from './fleet-issue-summary.svelte';

  const { Story } = defineMeta({
    title: 'Shell/Printers/FleetIssueSummary',
    component: FleetIssueSummary,
  });

  const onSelectedChange = () => undefined;
</script>

<Story
  name="Errors and notices"
  args={{
    counts: { error: 2, warning: 0, info: 1 },
    selected: [],
    onSelectedChange,
  }}
/>

<Story
  name="One chosen"
  args={{
    counts: { error: 2, warning: 3, info: 1 },
    selected: ['warning'],
    onSelectedChange,
  }}
/>

<Story
  name="Nothing wrong"
  args={{
    counts: { error: 0, warning: 0, info: 0 },
    selected: [],
    onSelectedChange,
  }}
/>
