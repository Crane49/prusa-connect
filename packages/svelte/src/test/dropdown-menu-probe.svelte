<!-- The printer actions dropdown menu the dropdown menu tests drive. -->
<script lang="ts">
  import Button from '../primitives/button/button.svelte';
  import {
    DropdownMenu,
    DropdownMenuCheckboxItem,
    DropdownMenuContent,
    DropdownMenuItem,
    DropdownMenuLabel,
    DropdownMenuSeparator,
    DropdownMenuShortcut,
    DropdownMenuTrigger,
  } from '../primitives/dropdown-menu';

  let {
    open = false,
    onRename = () => undefined,
    onRecalibrate = () => undefined,
  }: {
    open?: boolean;
    onRename?: () => void;
    onRecalibrate?: () => void;
  } = $props();

  let autoConnect = $state(false);
</script>

<DropdownMenu {open}>
  <DropdownMenuTrigger>
    {#snippet child({ props })}
      <Button {...props} variant="secondary">Printer actions</Button>
    {/snippet}
  </DropdownMenuTrigger>
  <DropdownMenuContent>
    <DropdownMenuLabel>Actions</DropdownMenuLabel>
    <DropdownMenuSeparator />
    <DropdownMenuItem onSelect={onRename}>
      Rename
      <DropdownMenuShortcut>⌘R</DropdownMenuShortcut>
    </DropdownMenuItem>
    <DropdownMenuItem>Duplicate</DropdownMenuItem>
    <DropdownMenuItem disabled onSelect={onRecalibrate}>
      Recalibrate
    </DropdownMenuItem>
    <DropdownMenuSeparator />
    <DropdownMenuCheckboxItem bind:checked={autoConnect}>
      Auto-connect
    </DropdownMenuCheckboxItem>
    <DropdownMenuSeparator />
    <DropdownMenuItem variant="destructive">Remove</DropdownMenuItem>
  </DropdownMenuContent>
</DropdownMenu>
