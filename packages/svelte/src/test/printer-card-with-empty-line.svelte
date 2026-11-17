<!-- PrinterCard with a card-line snippet that renders nothing, for the test
     that proves an empty line adds no space (I3). -->
<script lang="ts">
  import PrinterCard from '../shell/printers/printer-card.svelte';
  import type { PrinterCardProps } from '../shell/printers/types';

  let { row, shows }: Pick<PrinterCardProps, 'row' | 'shows'> = $props();
</script>

{#snippet line()}{/snippet}

<PrinterCard {row} {shows} {line} />
