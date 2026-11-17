<!--
  The caller a `PromptDialog` needs, asking for a folder name. The dialog holds
  no draft, so the demo owns the name, clears it on every path that shuts the
  panel, and refuses a name the folder already has.
-->
<script lang="ts">
  import { Button } from '../primitives/button';
  import { PromptDialog } from '../primitives/prompt-dialog';

  const TAKEN_NAMES = ['archive'];

  let open = $state(false);
  let value = $state('');
  let created = $state<string | undefined>();

  const trimmed = $derived(value.trim());
  const error = $derived(
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
  New folder
</Button>
{#if created}<p>{created} created.</p>{/if}
<PromptDialog
  bind:open
  onOpenChange={(next: boolean) => {
    if (!next) value = '';
  }}
  title="New folder"
  description="Creates an empty folder in the folder you're browsing."
  label="Folder name"
  placeholder="e.g. calibration"
  {value}
  onValueChange={(next: string) => {
    value = next;
  }}
  {error}
  confirmLabel="Create"
  onConfirm={() => {
    created = trimmed;
    open = false;
    value = '';
  }}
/>
