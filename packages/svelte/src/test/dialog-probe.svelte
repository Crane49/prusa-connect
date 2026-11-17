<!-- The rename dialog the dialog tests drive. -->
<script lang="ts">
  import Button from '../primitives/button/button.svelte';
  import {
    Dialog,
    DialogClose,
    DialogContent,
    DialogDescription,
    DialogFooter,
    DialogHeader,
    DialogTitle,
    DialogTrigger,
  } from '../primitives/dialog';

  import type { DialogSize } from '../primitives/dialog/types';

  let {
    open = false,
    showCloseButton = true,
    size = 'md',
  }: {
    open?: boolean;
    showCloseButton?: boolean;
    size?: DialogSize;
  } = $props();
</script>

<Dialog {open}>
  <DialogTrigger>
    {#snippet child({ props })}
      <Button {...props}>Rename printer</Button>
    {/snippet}
  </DialogTrigger>
  <DialogContent {showCloseButton} {size}>
    <DialogHeader>
      <DialogTitle>Rename printer</DialogTitle>
      <DialogDescription>
        Choose a name that helps you find this printer in a list.
      </DialogDescription>
    </DialogHeader>
    <DialogFooter>
      <DialogClose>
        {#snippet child({ props })}
          <Button {...props} variant="secondary">Cancel</Button>
        {/snippet}
      </DialogClose>
      <Button>Save</Button>
    </DialogFooter>
  </DialogContent>
</Dialog>
