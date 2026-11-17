<!-- The printer actions context menu the context menu tests drive. -->
<script lang="ts">
  import {
    ContextMenu,
    ContextMenuCheckboxItem,
    ContextMenuContent,
    ContextMenuGroup,
    ContextMenuItem,
    ContextMenuLabel,
    ContextMenuRadioGroup,
    ContextMenuRadioItem,
    ContextMenuSeparator,
    ContextMenuShortcut,
    ContextMenuSub,
    ContextMenuSubContent,
    ContextMenuSubTrigger,
    ContextMenuTrigger,
  } from '../primitives/context-menu';

  let {
    onRename = () => undefined,
    onRecalibrate = () => undefined,
    onMove = () => undefined,
  }: {
    onRename?: () => void;
    onRecalibrate?: () => void;
    onMove?: () => void;
  } = $props();

  let autoConnect = $state(false);
  let quality = $state('draft');
</script>

<ContextMenu>
  <ContextMenuTrigger tabindex={0}>Right-click this printer</ContextMenuTrigger>
  <ContextMenuContent>
    <ContextMenuLabel>Actions</ContextMenuLabel>
    <ContextMenuSeparator />
    <ContextMenuGroup>
      <ContextMenuItem onSelect={onRename}>
        Rename
        <ContextMenuShortcut>⌘R</ContextMenuShortcut>
      </ContextMenuItem>
      <ContextMenuItem>Duplicate</ContextMenuItem>
      <ContextMenuItem disabled onSelect={onRecalibrate}>
        Recalibrate
      </ContextMenuItem>
    </ContextMenuGroup>
    <ContextMenuSeparator />
    <ContextMenuCheckboxItem bind:checked={autoConnect}>
      Auto-connect
    </ContextMenuCheckboxItem>
    <ContextMenuSeparator />
    <ContextMenuSub>
      <ContextMenuSubTrigger>Move to</ContextMenuSubTrigger>
      <ContextMenuSubContent>
        <ContextMenuItem onSelect={onMove}>Workshop</ContextMenuItem>
      </ContextMenuSubContent>
    </ContextMenuSub>
    <ContextMenuSeparator />
    <ContextMenuRadioGroup bind:value={quality}>
      <ContextMenuRadioItem value="draft">Draft</ContextMenuRadioItem>
      <ContextMenuRadioItem value="fine">Fine</ContextMenuRadioItem>
    </ContextMenuRadioGroup>
    <ContextMenuSeparator />
    <ContextMenuItem variant="destructive">Remove</ContextMenuItem>
  </ContextMenuContent>
</ContextMenu>
