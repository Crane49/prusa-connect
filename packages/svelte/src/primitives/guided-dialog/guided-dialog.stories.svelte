<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import type { StepperStep } from '../stepper/types';
  import GuidedDialog from './guided-dialog.svelte';

  const steps: StepperStep[] = [
    { id: 'kind', label: 'What to add' },
    { id: 'address', label: 'Where it answers' },
    { id: 'name', label: 'What to call it' },
  ];

  const { Story } = defineMeta({
    title: 'Primitives/GuidedDialog',
    component: GuidedDialog,
    args: {
      open: true,
      title: 'Add a device',
      description: 'Three steps, and the machine is on the fleet.',
      steps,
      current: 1,
    },
  });
</script>

<Story name="Open">
  <p class="text-body">The address this server reaches the machine on.</p>
</Story>

<Story name="Closed" args={{ open: false }} />

<Story name="Nothing to continue with yet" args={{ canContinue: false }}>
  <p class="text-body">The address this server reaches the machine on.</p>
</Story>

<Story
  name="Waiting"
  args={{
    status: 'waiting',
    busy: true,
    waitingMessage: 'Waiting for the machine to answer.',
  }}
/>

<Story
  name="Done"
  args={{
    status: 'success',
    current: steps.length,
    endTitle: 'The printer is on the fleet',
    endMessage: 'It answers as Bench one.',
  }}
/>

<Story
  name="Failed"
  args={{
    status: 'failure',
    endTitle: 'The machine did not answer',
    endMessage: 'Check the address and try again.',
  }}
/>
