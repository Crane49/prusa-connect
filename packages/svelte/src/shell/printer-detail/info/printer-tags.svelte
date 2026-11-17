<!-- This draws the tags a printer carries, and the controls that change them. -->
<script lang="ts">
  import XIcon from '@lucide/svelte/icons/x';

  import { sameTag } from '../../../domain';
  import { t } from '../../../i18n';
  import { Badge, Button, PromptDialog } from '../../../primitives';
  import type { PrinterAdministration } from './types';

  let {
    tags,
    administration,
  }: {
    /** The list on the record the page holds. */
    tags: readonly string[];
    /** Without a store, the tags are only read and nothing here changes them. */
    administration?: PrinterAdministration | undefined;
  } = $props();

  // An edit on its way is drawn at once, so the list shown is the store's.
  const shown = $derived(administration?.tags(tags) ?? tags);

  let adding = $state(false);
  let draft = $state('');

  // Each edit says what to change, and the store applies it to the list the
  // server holds when the write's turn comes.
  const drop = (tag: string): void => {
    void administration?.editTags((current) =>
      current.filter((kept) => !sameTag(kept, tag)),
    );
  };

  // The server holds every other tag rule and answers a broken one by its
  // code. A tag the list already carries is caught here, so the list never
  // shows the same tag twice.
  const carried = $derived(
    shown.some((tag) => sameTag(tag, draft.trim()))
      ? t('printerDetailInfo.tags.carried')
      : undefined,
  );

  const add = (): void => {
    const tag = draft.trim();
    if (tag === '' || carried !== undefined) return;
    // Another person may have added the same tag in another spelling. Adding
    // it again would be refused, so the list is kept as it is.
    void administration?.editTags((current) =>
      current.some((held) => sameTag(held, tag)) ? current : [...current, tag],
    );
    adding = false;
    draft = '';
  };
</script>

<ul class="flex flex-wrap items-center gap-2">
  {#if shown.length === 0}
    <li class="text-sm text-muted-foreground">
      {t('printerDetailInfo.tags.none')}
    </li>
  {:else}
    {#each shown as tag (tag)}
      <li class="flex items-center gap-1">
        <Badge variant="outline">{tag}</Badge>
        {#if administration}
          <Button
            variant="ghost"
            size="icon-sm"
            label={t('printerDetailInfo.tags.remove', { tag })}
            onclick={() => {
              drop(tag);
            }}
          >
            <XIcon aria-hidden="true" />
          </Button>
        {/if}
      </li>
    {/each}
  {/if}
  {#if administration}
    <li>
      <Button
        variant="secondary"
        size="sm"
        onclick={() => {
          adding = true;
        }}
      >
        {t('printerDetailInfo.tags.add')}
      </Button>
    </li>
  {/if}
</ul>

{#if administration}
  <PromptDialog
    open={adding}
    onOpenChange={(next: boolean) => {
      adding = next;
      if (!next) draft = '';
    }}
    title={t('printerDetailInfo.tags.add')}
    description={t('printerDetailInfo.tags.form')}
    label={t('printerDetailInfo.tags.field')}
    value={draft}
    onValueChange={(next: string) => {
      draft = next;
    }}
    error={carried}
    confirmLabel={t('printerDetailInfo.tags.confirm')}
    onConfirm={add}
  />
{/if}
