<!--
  Every kind of card, to add one: its glyph, its name and one line on what it
  shows. The core's cards come first, then each module's, under the module's
  name. A pick adds the card where it first fits, with the settings its
  declaration starts from, and opens its settings, so a printer card asks for
  its printer at once.

  The list is read from the declarations the manifests made, so opening it
  loads no module's code (ADR-20261008-sww rule 25).
-->
<script lang="ts">
  import { MAX_CARDS } from '../../../domain';
  import { t } from '../../../i18n';
  import {
    Sheet,
    SheetContent,
    SheetDescription,
    SheetHeader,
    SheetTitle,
    Tile,
  } from '../../../primitives';
  import { defaultCardSettings } from '../../../slots';
  import type { CardKind } from '../card-catalogue';
  import { cardIcon } from '../cards/icons';
  import { dashboardContext } from '../context';

  let {
    open,
    onClose,
    onAdded,
  }: {
    open: boolean;
    onClose: () => void;
    /** Called with the card that was added, to open its settings. */
    onAdded: (id: string) => void;
  } = $props();

  const { editor, cards } = dashboardContext();

  const full = $derived((editor.draft?.cards.length ?? 0) >= MAX_CARDS);
  const kinds = $derived(cards.kinds);

  const add = (kind: CardKind): void => {
    const id = editor.add(
      kind.type,
      cards.sizeOf(kind.type),
      defaultCardSettings(kind.declaration),
    );
    onClose();
    if (id !== undefined) onAdded(id);
  };
</script>

<Sheet
  {open}
  onOpenChange={(next: boolean) => {
    if (!next) onClose();
  }}
>
  <SheetContent side="right" class="flex flex-col gap-4 overflow-y-auto">
    <SheetHeader>
      <SheetTitle>{t('dashboard.gallery.title')}</SheetTitle>
      <SheetDescription>
        {full
          ? t('dashboard.gallery.full', { most: MAX_CARDS })
          : t('dashboard.gallery.note')}
      </SheetDescription>
    </SheetHeader>
    <ul class="gallery grid gap-2 px-4" data-part="gallery">
      {#each kinds as kind, index (kind.type)}
        {@const Icon = cardIcon(kind.type, kind.declaration.icon)}
        {@const words = cards.wordsOf(kind.type)}
        {#if index > 0 && kind.namespace !== kinds[index - 1]?.namespace}
          <li
            class="gallery-group pbs-2 text-xs font-medium text-muted-foreground"
            aria-hidden="true"
          >
            {t('dashboard.gallery.from', {
              module: cards.moduleOf(kind.namespace),
            })}
          </li>
        {/if}
        <li>
          <Tile
            data-kind={kind.type}
            disabled={full}
            onclick={() => {
              add(kind);
            }}
          >
            <span class="flex items-start gap-3">
              <span
                class="flex size-9 shrink-0 items-center justify-center rounded-sm bg-secondary text-secondary-foreground"
              >
                <Icon aria-hidden="true" class="size-5" />
              </span>
              <span class="grid gap-0.5">
                <span class="text-sm font-medium"
                  >{words(kind.declaration.title)}</span
                >
                <span class="text-xs text-muted-foreground">
                  {words(kind.declaration.description)}
                </span>
              </span>
            </span>
          </Tile>
        </li>
      {/each}
    </ul>
  </SheetContent>
</Sheet>

<style>
  /* The sheet draws this in a portal, out of reach of a module's scoped
     utilities, so the few rules it needs are its own. */
  .gallery {
    padding-block-end: calc(var(--spacing) * 4);
  }
</style>
