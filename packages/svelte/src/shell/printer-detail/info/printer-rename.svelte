<!-- What an operator calls this printer, and the one control that changes it. -->
<script lang="ts">
  import PencilIcon from '@lucide/svelte/icons/pencil';

  import { t } from '../../../i18n';
  import { Button, PromptDialog } from '../../../primitives';
  import type { PrinterAdministration } from './types';

  let {
    name,
    administration,
    trigger = 'button',
  }: {
    name: string;
    administration: PrinterAdministration;
    /** A text button, or a pencil for where the name is already on screen. */
    trigger?: 'button' | 'icon';
  } = $props();

  /**
   * The bound the server holds a name to. It counts characters and not bytes,
   * so a name in Czech or in Japanese gets the same room as one in English.
   */
  const MAX_CHARACTERS = 96;

  let open = $state(false);
  let draft = $state('');

  const typed = $derived(draft.trim());
  const pending = $derived(administration.pending('rename'));

  // Code points, because the server counts Go runes and a name it refuses
  // must be a name this dialog refuses first.
  const tooLong = $derived(
    Array.from(typed).length > MAX_CHARACTERS
      ? t('printerDetailInfo.name.tooLong', { max: MAX_CHARACTERS })
      : undefined,
  );

  // The dialog stays open until the server answers. A refusal is drawn on
  // the card, and the dialog closes either way.
  // The dialog opens on what the printer is called now, so a person edits
  // the name instead of typing it again.
  const openDialog = (): void => {
    draft = name;
    open = true;
  };

  const rename = async (): Promise<void> => {
    await administration.rename(typed);
    open = false;
    draft = '';
  };
</script>

{#if trigger === 'icon'}
  <Button
    variant="ghost"
    size="icon-sm"
    label={t('printerDetailInfo.name.iconLabel')}
    onclick={openDialog}
  >
    <PencilIcon aria-hidden="true" />
  </Button>
{:else}
  <Button variant="secondary" size="sm" onclick={openDialog}>
    {t('printerDetailInfo.name.action')}
  </Button>
{/if}

<PromptDialog
  {open}
  onOpenChange={(next: boolean) => {
    if (pending) return;
    open = next;
    if (!next) draft = '';
  }}
  title={t('printerDetailInfo.name.title')}
  description={t('printerDetailInfo.name.form')}
  label={t('printerDetailInfo.name.field')}
  value={draft}
  onValueChange={(next: string) => {
    draft = next;
  }}
  error={tooLong}
  allowEmpty
  {pending}
  confirmLabel={t('printerDetailInfo.name.confirm')}
  onConfirm={() => {
    void rename();
  }}
/>
