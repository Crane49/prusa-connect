<!-- The pause button and its tooltip, for the tooltip tests. -->
<script lang="ts">
  import Button from '../primitives/button/button.svelte';
  import {
    Tooltip,
    TooltipContent,
    TooltipProvider,
    TooltipTrigger,
  } from '../primitives/tooltip';

  let { open = false }: { open?: boolean } = $props();
</script>

<TooltipProvider>
  <Tooltip {open}>
    <TooltipTrigger>
      {#snippet child({ props })}
        <Button {...props} variant="secondary">Pause</Button>
      {/snippet}
    </TooltipTrigger>
    <TooltipContent>Pause the current job</TooltipContent>
  </Tooltip>
</TooltipProvider>
