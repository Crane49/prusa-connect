<!--
  Adds a version of the part: the files it is at now, and a note on what
  changed.

  The form inside is made new each time the dialog opens, with a draft of its
  own. Closing the dialog without saving takes the form away, and the form
  throws its draft away as it goes, which deletes every file it uploaded.

  While the version is being saved the dialog holds itself open: Escape, a
  click outside and the close control do nothing, so the files being recorded
  are never deleted under the version that names them.
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
  import VersionForm from './version-form.svelte';

  let {
    open,
    part,
    dropped = [],
    onClose,
    onSaved,
  }: {
    open: boolean;
    part: PartDetail;
    /** Files dropped onto the part's page, handed to the form it opens with. */
    dropped?: readonly File[];
    onClose: () => void;
    onSaved: () => void;
  } = $props();

  const t = heldSession().host.t;

  // The module never gives a number out twice, also one whose version was
  // deleted since, so the next number follows the highest it ever gave.
  const nextNumber = $derived(
    Math.max(part.last_version ?? 0, part.versions.at(-1)?.number ?? 0) + 1,
  );

  /** True while the form is saving, which holds the dialog open. */
  let saving = $state(false);

  const close = (): void => {
    if (saving) return;
    onClose();
  };
</script>

<Dialog
  {open}
  onOpenChange={(next: boolean) => {
    if (!next) close();
  }}
>
  <DialogContent
    escapeKeydownBehavior={saving ? 'ignore' : 'close'}
    interactOutsideBehavior={saving ? 'ignore' : 'close'}
    showCloseButton={!saving}
  >
    <DialogHeader>
      <DialogTitle>
        {t('parts.versionDialog.title', {
          number: nextNumber,
        })}
      </DialogTitle>
      <DialogDescription>{t('parts.versionDialog.note')}</DialogDescription>
    </DialogHeader>
    {#if open}
      <VersionForm
        {part}
        {dropped}
        onSaving={(next: boolean) => {
          saving = next;
        }}
        onCancel={close}
        onSaved={() => {
          saving = false;
          onClose();
          onSaved();
        }}
      />
    {/if}
  </DialogContent>
</Dialog>
