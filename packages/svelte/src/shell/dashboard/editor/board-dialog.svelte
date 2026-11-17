<!--
  Makes a dashboard, or names one again. The fields are in the form, which is
  made new for each act (acts.ts).
-->
<script lang="ts">
  import {
    Dialog,
    DialogContent,
    DialogDescription,
    DialogHeader,
    DialogTitle,
  } from '../../../primitives';

  import { t } from '../../../i18n';
  import type { DashboardAct } from './acts';
  import BoardForm from './board-form.svelte';

  let { act, onClose }: { act: DashboardAct | undefined; onClose: () => void } =
    $props();
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
        {act?.kind === 'rename'
          ? t('dashboard.dialog.renameTitle')
          : t('dashboard.dialog.newTitle')}
      </DialogTitle>
      <DialogDescription>
        {act?.kind === 'rename'
          ? t('dashboard.dialog.renameNote')
          : t('dashboard.dialog.newNote')}
      </DialogDescription>
    </DialogHeader>
    {#if act}
      {#key act}
        <BoardForm {act} {onClose} />
      {/key}
    {/if}
  </DialogContent>
</Dialog>
