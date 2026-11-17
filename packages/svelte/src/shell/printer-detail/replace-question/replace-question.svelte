<!--
  The question a file sent to this printer asks when the printer already holds
  a file under its name. The printer's file is deleted only on Replace, so
  Cancel and closing the dialog both keep it.

  A file sent to be printed has three answers and no sentence: the buttons
  say each one. Replace and print sends this file in place of the printer's,
  Print the printer's file starts the one already there and sends nothing,
  and Cancel print does neither.
-->
<script lang="ts">
  import { t } from '../../../i18n';
  import { ConfirmDialog } from '../../../primitives';
  import AlertDialog from '../../../primitives/alert-dialog/alert-dialog.svelte';
  import AlertDialogAction from '../../../primitives/alert-dialog/alert-dialog-action.svelte';
  import AlertDialogCancel from '../../../primitives/alert-dialog/alert-dialog-cancel.svelte';
  import AlertDialogContent from '../../../primitives/alert-dialog/alert-dialog-content.svelte';
  import AlertDialogHeader from '../../../primitives/alert-dialog/alert-dialog-header.svelte';
  import AlertDialogTitle from '../../../primitives/alert-dialog/alert-dialog-title.svelte';
  import type { ReplaceQuestionProps } from './types';

  let { name, print, onReplace, onPrintHeld, onKeep }: ReplaceQuestionProps =
    $props();

  const title = $derived(
    t('printerDetailFiles.replace.title', { name: name ?? '' }),
  );
</script>

{#if print === true && onPrintHeld !== undefined}
  <AlertDialog
    open={name !== undefined}
    onOpenChange={(open: boolean) => {
      if (!open) onKeep();
    }}
  >
    <AlertDialogContent>
      <AlertDialogHeader>
        <AlertDialogTitle>{title}</AlertDialogTitle>
      </AlertDialogHeader>
      <!-- Three answers do not fit the footer's row, so they stay stacked at
           every width: printing the printer's file on top, as the one that
           loses nothing, then replacing it, then cancelling. The footer's own classes set a row
           on a wide window, so this is a column of its own. -->
      <div
        data-slot="replace-question-answers"
        class="flex flex-col-reverse gap-2"
      >
        <AlertDialogCancel variant="ghost" size="lg">
          {t('printerDetailFiles.replace.cancelPrint')}
        </AlertDialogCancel>
        <AlertDialogAction variant="secondary" size="lg" onclick={onReplace}>
          {t('printerDetailFiles.replace.replacePrint')}
        </AlertDialogAction>
        <AlertDialogAction variant="secondary" size="lg" onclick={onPrintHeld}>
          {t('printerDetailFiles.replace.printHeld')}
        </AlertDialogAction>
      </div>
    </AlertDialogContent>
  </AlertDialog>
{:else}
  <ConfirmDialog
    open={name !== undefined}
    onOpenChange={(open: boolean) => {
      if (!open) onKeep();
    }}
    {title}
    description={t('printerDetailFiles.replace.note')}
    confirmLabel={t('printerDetailFiles.replace.confirm')}
    onConfirm={onReplace}
  />
{/if}
