<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import StepList from './step-list.svelte';
  import type { StepListStep } from './types';

  const { Story } = defineMeta({
    title: 'Primitives/StepList',
    component: StepList,
  });

  const steps: StepListStep[] = [
    { id: 'file', label: 'Prepare the file', description: 'Download it' },
    { id: 'printer', label: 'On the printer', description: 'A few taps' },
    { id: 'approve', label: 'Approve the printer', description: 'Check it' },
    { id: 'done', label: 'Connected', description: 'Ready to print' },
  ];
</script>

<Story
  name="Connected"
  args={{ label: 'Steps', steps, current: 1, connected: true }}
/>

<Story
  name="Pickable"
  args={{ label: 'Steps', steps, current: 2, onpick: () => undefined }}
/>

<Story
  name="Reached Ahead"
  args={{
    label: 'Steps',
    steps,
    current: 1,
    reached: 3,
    onpick: () => undefined,
  }}
/>
