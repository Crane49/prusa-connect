<!-- PrinterCard with a card-line snippet, for the test to prove it draws. -->
<script lang="ts">
  import PrinterCard from '../shell/printers/printer-card.svelte';
  import type { PrinterCardProps } from '../shell/printers/types';

  let { row, shows }: Pick<PrinterCardProps, 'row' | 'shows'> = $props();
</script>

{#snippet line(deviceId: string)}
  <span data-testid="card-line">{deviceId}</span>
{/snippet}

<PrinterCard {row} {shows} {line} />
