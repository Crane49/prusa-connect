<!-- A three-step guided dialog that owns its own step, for the guided-dialog
     tests. Next and Back move the step the way a screen moves it, so what the
     tests watch is a real press and not a prop written from the outside. -->
<script lang="ts">
  import GuidedDialog from '../primitives/guided-dialog/guided-dialog.svelte';
  import type { StepperStep } from '../primitives/stepper/types';

  const steps: StepperStep[] = [
    { id: 'name', label: 'Name it' },
    { id: 'connect', label: 'Connect it' },
    { id: 'check', label: 'Check it' },
  ];

  let current = $state(0);
</script>

<GuidedDialog
  open={true}
  title="Add a device"
  {steps}
  {current}
  onnext={() => {
    current += 1;
  }}
  onback={() => {
    current -= 1;
  }}
>
  <p data-slot="guided-probe-step">Question {current + 1}</p>
</GuidedDialog>
