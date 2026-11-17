<!--
  Makes a folder, or renames, moves and describes one.

  One dialog for both, because the fields are the same three and a person who
  renames a folder may as well say what it holds. The form inside is made new
  each time the dialog opens on an act, so its fields start from that act and
  nothing typed into an abandoned one comes back.
-->
<script lang="ts">
  import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogHeader,
    DialogTitle,
  } from '@prusa/connect-ui/primitives';

  import { heldSession } from '../session';
  import type { FolderAct } from './folder-act';
  import FolderForm from './folder-form.svelte';

  let { act, onClose }: { act: FolderAct | undefined; onClose: () => void } =
    $props();

  const t = heldSession().host.t;
</script>

<Dialog
  open={act !== undefined}
  onOpenChange={(next: boolean) => {
    if (!next) onClose();
  }}
>
  <DialogContent>
    <DialogHeader>
      <DialogTitle>
        {act?.kind === 'change'
          ? t('parts.folder.editTitle')
          : t('parts.folder.newTitle')}
      </DialogTitle>
      <DialogDescription>{t('parts.folder.note')}</DialogDescription>
    </DialogHeader>
    {#if act}
      {#key act}
        <FolderForm {act} {onClose} />
      {/key}
    {/if}
  </DialogContent>
</Dialog>
