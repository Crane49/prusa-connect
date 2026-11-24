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

<!-- Several files: one of them is printed and goes first, the rest are
     copied. The person picks which. -->
<Story
  name="Several Files"
  args={{
    names: [
      'Bracket_Left_Reinforced_v4_0.4n_0.2mm_PETG_MK4S_1h12m.bgcode',
      'Bracket_Right_Reinforced_v4_0.4n_0.2mm_PETG_MK4S_1h12m.bgcode',
      'Cable_Clip_x10_0.4n_0.2mm_PLA_MK4S_24m.bgcode',
    ],
    printer: 'MK4 by the window',
    printTarget: free,
    ...handlers,
  }}
/>

<!-- Several files on a busy printer are only copied, and it says why. -->
<Story
  name="Several Files On A Printing Printer"
  args={{
    names: ['bracket.bgcode', 'hinge.bgcode', 'lid.bgcode'],
    printer: 'MK4 by the window',
    printTarget: printing,
    ...handlers,
  }}
/>

<!-- One firmware image on a printer that can flash: the dialog starts on
     Flash firmware, and the person can pick to only copy it. -->
<Story
  name="A Firmware Image To Flash"
  args={{
    names: ['MK4_MK4S_MK3.9_MK3.9S_firmware_8.0.2.bbf'],
    printer: 'MK4 by the window',
    printTarget: free,
    onFlash: () => undefined,
    ...handlers,
  }}
/>

<!-- Flash greyed with the reason, after the printer stopped being able to
     flash while the dialog was open, has no story: it needs the printer to
     change under an open dialog, which the component's tests drive. -->

<!-- A file that cannot be printed on a printer that cannot flash it: copying
     is the one choice, so there is nothing to pick. -->
<Story
  name="A File That Can Only Be Copied"
  args={{
    names: ['MK4_MK4S_MK3.9_MK3.9S_firmware_8.0.2.bbf'],
    printer: 'MK4 by the window',
    printTarget: printing,
    ...handlers,
  }}
/>
