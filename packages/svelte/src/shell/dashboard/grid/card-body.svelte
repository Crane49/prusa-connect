<!--
  What one card draws: the component its type registered, handed settings
  held to the type's schema (ADR-20261008-sww rules 19 and 21).

  Four answers before the card itself. A type no installed module declares
  is a placeholder that keeps the card (rule 23), once the modules' listing
  was read; before that the type may still be declared, so it is loading. A
  listing that could not be read says nothing about the type, so the card
  says it could not be read and offers to read it again. A
  type whose module's bundle is still on its way is a skeleton, and the card
  asks for that bundle itself, so only the modules of the cards drawn are
  loaded (rule 25). A
  bundle that loaded and registered no such card, and a card that throws,
  are an error card naming its type, and the rest of the page draws on
  (rule 22).
-->
<script lang="ts">
  import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';
  import { onMount } from 'svelte';

  import type { DashboardCard } from '../../../domain';
  import { t } from '../../../i18n';
  import { Button, EmptyState, Skeleton } from '../../../primitives';
  import { CORE_CARD_NAMESPACE, readCardSettings } from '../../../slots';
  import UnknownCard from '../cards/unknown-tile.svelte';
  import { dashboardContext } from '../context';

  let { card, editing }: { card: DashboardCard; editing: boolean } = $props();

  const { cards } = dashboardContext();

  const kind = $derived(cards.kind(card.type));
  const registered = $derived(cards.card(card.type));
  // Held to the schema here, once per change of the card, so a card never
  // reads a value its schema does not allow.
  const config = $derived(
    kind === undefined
      ? {}
      : readCardSettings(kind.declaration.settings, card.config),
  );
  const size = $derived({ w: card.w, h: card.h });

  /** The module's bundle was asked for and the card is still not there. */
  let missing = $state(false);
  /** The modules' listing could not be read when this card asked for its bundle. */
  let unread = $state(false);
  let gone = false;

  const load = async (): Promise<void> => {
    if (cards.card(card.type) !== undefined) return;
    await cards.load([card.type]);
    if (gone) return;
    unread = cards.unread;
    missing = !unread && cards.card(card.type) === undefined;
  };

  /** Reads the modules' listing again, then the bundle the card needs. */
  const retry = async (): Promise<void> => {
    unread = false;
    await cards.discover();
    if (!gone) await load();
  };

  onMount(() => {
    void load();

    return () => {
      gone = true;
    };
  });
</script>

{#snippet failure()}
  <EmptyState
    size="sm"
    icon={TriangleAlertIcon}
    heading={t('dashboard.card.failed')}
    description={t('dashboard.card.failedNote', {
      kind: cards.titleOf(card.type),
    })}
  />
{/snippet}

{#snippet unreadCard()}
  <EmptyState
    size="sm"
    icon={TriangleAlertIcon}
    heading={t('dashboard.card.unread.heading')}
    description={t('dashboard.card.unread.note')}
  >
    {#snippet action()}
      <Button
        variant="secondary"
        size="sm"
        data-part="card-retry"
        onclick={() => void retry()}
      >
        {t('dashboard.card.unread.retry')}
      </Button>
    {/snippet}
  </EmptyState>
{/snippet}

{#snippet loading()}
  <div
    role="status"
    aria-label={t('dashboard.card.loading')}
    class="flex flex-1 flex-col gap-2"
  >
    <Skeleton class="h-5 w-2/3" />
    <Skeleton class="w-full flex-1" />
  </div>
{/snippet}

{#if kind === undefined && !cards.listed}
  {@render loading()}
{:else if unread || (kind === undefined && cards.unread)}
  {@render unreadCard()}
{:else if kind === undefined}
  <UnknownCard type={card.type} />
{:else if registered === undefined && missing}
  {@render failure()}
{:else if registered === undefined}
  {@render loading()}
{:else}
  {@const Card = registered.component}
  <svelte:boundary>
    <!-- A module's card sits inside its namespace, which is where the
         module's own styles reach (ADR-20260911-4p6, a module's CSS is scoped). -->
    <div
      class="flex min-h-0 flex-1 flex-col"
      data-module={kind.namespace === CORE_CARD_NAMESPACE
        ? undefined
        : kind.namespace}
    >
      <Card {config} title={card.title} {size} {editing} />
    </div>
    {#snippet failed()}
      {@render failure()}
    {/snippet}
  </svelte:boundary>
{/if}
