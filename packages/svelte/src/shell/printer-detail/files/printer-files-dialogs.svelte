<!-- The three questions this panel asks before it changes a printer's drive. -->
<script lang="ts">
  import { t } from '../../../i18n';
  import { ConfirmDialog, PromptDialog } from '../../../primitives';
  import type { PrinterFileEntry } from './types';

  let {
    creating,
    folderName,
    folderFault,
    flashing,
    deleting,
    deleteTitle,
    deleteNote,
    nameOf,
    onCreatingChange,
    onFolderName,
    onCreate,
    onFlashCancel,
    onFlash,
    onDeleteCancel,
    onDelete,
  }: {
    creating: boolean;
    folderName: string;
    folderFault?: string | undefined;
    flashing?: PrinterFileEntry | undefined;
    /** The entries a confirmed Delete would remove. Empty closes the dialog. */
    deleting: readonly PrinterFileEntry[];
    deleteTitle: string;
    deleteNote: string;
    nameOf: (entry: PrinterFileEntry) => string;
    onCreatingChange: (open: boolean) => void;
    onFolderName: (next: string) => void;
    onCreate: () => void;
    onFlashCancel: () => void;
    onFlash: () => void;
    onDeleteCancel: () => void;
    onDelete: () => void;
  } = $props();
</script>

<PromptDialog
  open={creating}
  onOpenChange={onCreatingChange}
  title={t('printerDetailFiles.newFolder.title')}
  description={t('printerDetailFiles.newFolder.note')}
  label={t('printerDetailFiles.newFolder.label')}
  value={folderName}
  onValueChange={onFolderName}
  error={folderFault}
  confirmLabel={t('printerDetailFiles.action.create')}
  onConfirm={onCreate}
/>

<!-- `core.command.flash` is irreversible. -->
<ConfirmDialog
  open={flashing !== undefined}
  onOpenChange={(open) => {
    if (!open) onFlashCancel();
  }}
  title={t('printerDetail.files.flash.title', {
    name: flashing === undefined ? '' : nameOf(flashing),
  })}
  description={t('printerDetail.files.flash.note')}
  confirmLabel={t('printerDetail.files.action.flash')}
  onConfirm={onFlash}
/>

<ConfirmDialog
  open={deleting.length > 0}
  onOpenChange={(open) => {
    if (!open) onDeleteCancel();
  }}
  title={deleteTitle}
  description={deleteNote}
  confirmLabel={t('printerDetailFiles.action.delete')}
  onConfirm={onDelete}
/>
