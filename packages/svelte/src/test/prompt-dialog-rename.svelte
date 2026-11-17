<!--
  The rename case. Empty here is the name the file already has, not a blank
  field, so shutting the panel puts that name back rather than clearing it.
-->
<script lang="ts">
  import { Button } from '../primitives/button';
  import { PromptDialog } from '../primitives/prompt-dialog';

  const TAKEN_NAMES = ['archive'];

  let { currentName = 'benchy.3mf' }: { currentName?: string } = $props();

  let open = $state(false);
  // svelte-ignore state_referenced_locally
  let value = $state(currentName);
  let renamedTo = $state<string | undefined>();

  const trimmed = $derived(value.trim());
  const error = $derived(
    trimmed.toLowerCase() !== currentName.toLowerCase() &&
      TAKEN_NAMES.includes(trimmed.toLowerCase())
      ? 'A file or folder with that name already exists.'
      : undefined,
  );
</script>

<Button
  onclick={() => {
    open = true;
  }}
>
  Rename
</Button>
{#if renamedTo}<p>Renamed to {renamedTo}.</p>{/if}
<PromptDialog
  bind:open
  onOpenChange={(next: boolean) => {
    if (!next) value = currentName;
  }}
  title={`Rename ${currentName}`}
  description="Choose a new name for this file or folder."
  label="Name"
  {value}
  onValueChange={(next: string) => {
    value = next;
  }}
  {error}
  confirmLabel="Rename"
  onConfirm={() => {
    renamedTo = trimmed;
    open = false;
    value = currentName;
  }}
/>
