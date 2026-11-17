<!--
  The settings of one card, in a panel at the side, so the card stays in
  sight and every change shows on it as it is made.

  The heading over the card is the same for every kind. Under it is the form
  the core draws from the card's schema, or the card's own editor when its
  module registered one (ADR-20261008-sww rules 19 and 20). What an editor hands back
  is held to the schema before the draft keeps it, so an editor cannot store
  a value its card would not read.

  Every change goes into the draft as it is made, and a change of the same
  card soon after the last one is the same step back, so a sentence typed
  into a note is one step. The card shows each change at once. Done closes
  the panel, and Remove takes away a card added by mistake. Nothing reaches
  the core until the dashboard is saved.
-->
<script lang="ts">
  import { t } from '../../../i18n';
  import TrashIcon from '@lucide/svelte/icons/trash';

  import {
    Button,
    Input,
    Label,
    Sheet,
    SheetContent,
    SheetDescription,
    SheetFooter,
    SheetHeader,
    SheetTitle,
  } from '../../../primitives';
  import { type CardSettings, readCardSettings } from '../../../slots';
  import { dashboardContext } from '../context';
  import SettingsForm from '../settings/settings-form.svelte';

  let {
    id,
    onClose,
    onCloseAutoFocus,
  }: {
    id: string | undefined;
    onClose: () => void;
    /** Where the focus goes once the panel has closed; the panel's default unless it cancels the event. */
    onCloseAutoFocus: (event: Event) => void;
  } = $props();

  const { editor, cards } = dashboardContext();
  const uid = $props.id();

  const card = $derived(id === undefined ? undefined : editor.card(id));
  const kind = $derived(card === undefined ? undefined : cards.kind(card.type));
  const words = $derived(
    card === undefined ? (text: string) => text : cards.wordsOf(card.type),
  );
  const Editor = $derived(
    card === undefined ? undefined : cards.card(card.type)?.editor,
  );
  const settings = $derived(
    card === undefined || kind === undefined
      ? undefined
      : readCardSettings(kind.declaration.settings, card.config),
  );

  const keep = (cardId: string, next: CardSettings): void => {
    if (kind === undefined) return;
    editor.configure(cardId, readCardSettings(kind.declaration.settings, next));
  };
</script>

<Sheet
  open={card !== undefined}
  onOpenChange={(next: boolean) => {
    if (!next) onClose();
  }}
>
  <SheetContent
    side="right"
    class="flex flex-col gap-4 overflow-y-auto"
    {onCloseAutoFocus}
  >
    {#if card !== undefined}
      <SheetHeader>
        <SheetTitle>{cards.titleOf(card.type)}</SheetTitle>
        <SheetDescription>
          {kind === undefined
            ? t('dashboard.card.unknownNote')
            : words(kind.declaration.description)}
        </SheetDescription>
      </SheetHeader>
      <div class="card-form grid gap-4 px-4" data-part="card-form">
        <div class="grid gap-2">
          <Label for="{uid}-title">{t('dashboard.settings.title')}</Label>
          <Input
            id="{uid}-title"
            autocomplete="off"
            placeholder={t('dashboard.settings.titleNote')}
            value={card.title}
            oninput={(event: Event & { currentTarget: HTMLInputElement }) => {
              editor.retitle(card.id, event.currentTarget.value);
            }}
          />
        </div>
        {#if kind !== undefined && settings !== undefined}
          {#if Editor !== undefined}
            <!-- The editor is the module's, so it sits inside its namespace,
                 where the module's own styles reach. -->
            <div data-module={kind.namespace}>
              <Editor
                config={settings}
                onchange={(next: CardSettings) => {
                  keep(card.id, next);
                }}
              />
            </div>
          {:else}
            <SettingsForm
              schema={kind.declaration.settings}
              config={settings}
              label={words}
              onchange={(next: CardSettings) => {
                keep(card.id, next);
              }}
            />
          {/if}
        {/if}
      </div>
      <!-- The footer stays at the bottom of the panel while the form scrolls,
           so Done is in reach after any field. -->
      <SheetFooter
        class="sticky inset-be-0 flex-row justify-between gap-2 border-bs border-border bg-background"
      >
        <Button
          variant="ghost"
          data-part="card-remove"
          onclick={() => {
            editor.remove(card.id);
            onClose();
          }}
        >
          <TrashIcon aria-hidden="true" />
          {t('dashboard.settings.remove')}
        </Button>
        <Button data-part="card-done" onclick={onClose}>
          {t('dashboard.settings.done')}
        </Button>
      </SheetFooter>
    {/if}
  </SheetContent>
</Sheet>

<style>
  /* The sheet draws this in a portal, out of reach of a module's scoped
     utilities, so the few rules it needs are its own. */
  .card-form {
    padding-block-end: calc(var(--spacing) * 4);
  }
</style>
