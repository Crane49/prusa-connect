<!-- The printer options popover the popover tests drive. -->
<script lang="ts">
  import Button from '../primitives/button/button.svelte';
  import {
    Popover,
    PopoverContent,
    PopoverDescription,
    PopoverHeader,
    PopoverTitle,
    PopoverTrigger,
  } from '../primitives/popover';

  let { open = false }: { open?: boolean } = $props();
</script>

<Popover {open}>
  <PopoverTrigger>
    {#snippet child({ props })}
      <Button {...props} variant="secondary">Printer options</Button>
    {/snippet}
  </PopoverTrigger>
  <PopoverContent>
    <PopoverHeader>
      <PopoverTitle>Printer options</PopoverTitle>
      <PopoverDescription>
        Settings apply to this printer only.
      </PopoverDescription>
    </PopoverHeader>
    <Button size="sm" class="mt-3">Open settings</Button>
  </PopoverContent>
</Popover>
