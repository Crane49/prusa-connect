<!-- A combobox inside an open dialog, for the Escape test. -->
<script lang="ts">
  import {
    Dialog,
    DialogContent,
    DialogHeader,
    DialogTitle,
  } from '../primitives/dialog';
  import Combobox from '../primitives/combobox/combobox.svelte';

  let { title }: { title: string } = $props();
</script>

<Dialog open>
  <DialogContent>
    <DialogHeader>
      <DialogTitle>{title}</DialogTitle>
    </DialogHeader>
    <Combobox
      items={[{ value: '1', label: 'Shelf A' }]}
      value={undefined}
      onPick={() => undefined}
      label="Location"
    />
  </DialogContent>
</Dialog>
