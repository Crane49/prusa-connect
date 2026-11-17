<!--
  Adds a part, or changes the words of one: its name, the line under it, the
  folder it is in and its tags. A new part also takes its description here;
  an existing one edits it on its own page, where there is room for it.

  The form is made new each time the dialog opens, so its fields start from
  the part on screen.
-->
<script lang="ts">
  import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogHeader,
    DialogTitle,
  } from '@prusa/connect-ui/primitives';

  import type { PartDetail } from '../parts';
  import { heldSession } from '../session';
  import PartForm from './part-form.svelte';

  let {
    open,
    folder,
    part,
    onClose,
  }: {
    open: boolean;
    /** The folder a new part goes into. */
    folder: number;
    /** The part being changed, and undefined for a new one. */
    part?: PartDetail | undefined;
    onClose: () => void;
  } = $props();

  const t = heldSession().host.t;
</script>

<Dialog
  {open}
  onOpenChange={(next: boolean) => {
    if (!next) onClose();
  }}
>
  <DialogContent>
    <DialogHeader>
      <DialogTitle>
        {part === undefined
          ? t('parts.part.newTitle')
          : t('parts.part.editTitle')}
      </DialogTitle>
      <DialogDescription>
        {part === undefined
          ? t('parts.part.newNote')
          : t('parts.part.editNote')}
      </DialogDescription>
    </DialogHeader>
    {#if open}
      <PartForm {folder} {part} {onClose} />
    {/if}
  </DialogContent>
</Dialog>
