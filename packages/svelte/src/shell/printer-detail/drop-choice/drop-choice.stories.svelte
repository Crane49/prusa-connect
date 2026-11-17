<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import type { PrintTarget } from '../../print-picker';
  import DropChoice from './drop-choice.svelte';

  const { Story } = defineMeta({
    title: 'Parts/Screen components/Printer Detail/Drop Choice',
    component: DropChoice,
  });

  const free: PrintTarget = {
    id: 'p1',
    model: 'MK4',
    name: 'MK4 by the window',
    ready: true,
    reasonKey: undefined,
    reasonStates: undefined,
    state: 'IDLE',
  };

  const printing: PrintTarget = {
    ...free,
    ready: false,
    reasonKey: 'printPicker.reason.state.PRINTING',
    state: 'PRINTING',
  };

  const handlers = {
    onConfirm: () => undefined,
    onCancel: () => undefined,
  };
</script>

<!-- One file on a printer that can start it: the dialog starts on Print now. -->
<Story
  name="One File On A Free Printer"
  args={{
    names: ['Bracket_Left_Reinforced_v4_FINAL.bgcode'],
    printer: 'MK4 by the window',
    printTarget: free,
    ...handlers,
  }}
/>

<!-- The printer is busy, so the file can only be copied, and it says why. -->
<Story
  name="The Printer Is Printing"
  args={{
    names: ['bracket.bgcode'],
    printer: 'MK4 by the window',
    printTarget: printing,
    ...handlers,
  }}
/>

<!-- Several files at once are only copied: a printer prints one at a time. -->
<Story
  name="Several Files"
  args={{
    names: ['bracket.bgcode', 'hinge.bgcode', 'lid.bgcode'],
    printer: 'MK4 by the window',
    printTarget: free,
    ...handlers,
  }}
/>
