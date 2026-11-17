<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import Stepper from './stepper.svelte';
  import type { StepperStep } from './types';

  const { Story } = defineMeta({
    title: 'Primitives/Stepper',
    component: Stepper,
  });

  const steps: StepperStep[] = [
    { id: 'mode', label: 'How people sign in', description: 'Open or asked' },
    { id: 'account', label: 'The first account' },
    { id: 'modules', label: 'What to install' },
    { id: 'review', label: 'Check and finish' },
  ];
</script>

<Story name="Default" args={{ steps, current: 1 }} />

<Story name="First step" args={{ steps, current: 0 }} />

<Story name="Finished" args={{ steps, current: steps.length }} />

<Story name="Compact" args={{ steps, current: 1, compact: true }} />
