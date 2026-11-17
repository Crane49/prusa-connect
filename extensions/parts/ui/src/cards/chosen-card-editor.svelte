<!--
  The settings of a card of chosen parts: the parts on the card, in the order
  they were chosen, and a search of the catalogue to add more. The schema can
  only keep the ids as a line of text, so the module draws this instead of
  the core's form (ADR-20261008-hna rule 20).
-->
<script lang="ts">
  import PlusIcon from '@lucide/svelte/icons/plus';
  import XIcon from '@lucide/svelte/icons/x';
  import {
    Button,
    FaultAlert,
    Input,
    Label,
  } from '@prusa/connect-ui/primitives';
  import type { CardEditorProps } from '@prusa/connect-ui/slots';
  import { onMount } from 'svelte';

  import { faultText } from '../faults';
  import { partsOn } from '../parts';
  import { heldSession } from '../session';
  import { CardParts, type CardPart } from './card-parts.svelte';
  import { PartSearch } from './part-search.svelte';
  import { idsOf, MAX_CHOSEN, writtenIds } from './settings';

  let { config, onchange }: CardEditorProps = $props();

  const { host } = heldSession();
  const t = host.t;
  const uid = $props.id();
  const parts = partsOn(host);
  const known = new CardParts(parts);
  const search = new PartSearch(parts);

  /** The names of parts added while the editor is open, by id. */
  let added = $state.raw<readonly CardPart[]>([]);

  const ids = $derived(idsOf(config.parts));
  const full = $derived(ids.length >= MAX_CHOSEN);

  onMount(() => {
    void known.readChosen(idsOf(config.parts));
    search.search('');

    return () => {
      known.stop();
      search.stop();
    };
  });

  const nameOf = (id: number): string =>
    [...added, ...known.rows].find((part) => part.id === id)?.name ??
    t('parts.dashboard.chosen.unknown', { id });

  const write = (next: readonly number[]): void => {
    onchange({ ...config, parts: writtenIds(next) });
  };
</script>

<div class="grid gap-y-4" data-part="parts-chosen-editor">
  <div class="grid gap-2">
    <h3 class="text-sm font-medium">{t('parts.dashboard.chosen.on')}</h3>
    {#if known.fault !== undefined}
      <FaultAlert fault={faultText(known.fault, t)} />
    {/if}
    {#if ids.length === 0}
      <p class="text-sm text-muted-foreground">
        {t('parts.dashboard.chosen.noneYet')}
      </p>
    {:else}
      <ul class="grid gap-1" data-part="chosen-list">
        {#each ids as id (id)}
          <li
            class="flex items-center justify-between gap-2 rounded-sm border border-border px-2 py-1"
          >
            <span class="min-w-0 truncate text-sm">{nameOf(id)}</span>
            <Button
              variant="ghost"
              size="icon-sm"
              label={t('parts.dashboard.chosen.remove', {
                name: nameOf(id),
              })}
              onclick={() => {
                write(ids.filter((held) => held !== id));
              }}
            >
              <XIcon aria-hidden="true" />
            </Button>
          </li>
        {/each}
      </ul>
    {/if}
  </div>

  <div class="grid gap-2">
    <Label for="{uid}-search">{t('parts.dashboard.chosen.search')}</Label>
    <Input
      id="{uid}-search"
      type="search"
      autocomplete="off"
      placeholder={t('parts.dashboard.chosen.searchNote')}
      oninput={(event: Event & { currentTarget: HTMLInputElement }) => {
        search.search(event.currentTarget.value);
      }}
    />
    {#if full}
      <p class="text-xs text-muted-foreground">
        {t('parts.dashboard.chosen.full', { most: MAX_CHOSEN })}
      </p>
    {/if}
    {#if search.fault !== undefined}
      <FaultAlert fault={faultText(search.fault, t)} />
    {:else if search.searched && !search.loading && search.rows.length === 0}
      <p class="text-sm text-muted-foreground" data-part="search-none">
        {t('parts.empty.search.heading')}
      </p>
    {:else}
      <ul
        class="grid gap-1"
        aria-busy={search.loading}
        data-part="search-results"
      >
        {#each search.rows as part (part.id)}
          {@const chosen = ids.includes(part.id)}
          <li class="flex items-center justify-between gap-2 px-2 py-1">
            <span class="min-w-0 truncate text-sm">{part.name}</span>
            <Button
              variant="secondary"
              size="sm"
              disabled={chosen || full}
              aria-label={t('parts.dashboard.chosen.add', { name: part.name })}
              onclick={() => {
                added = [...added, part];
                write([...ids, part.id]);
              }}
            >
              <PlusIcon aria-hidden="true" />
              {chosen
                ? t('parts.dashboard.chosen.chosen')
                : t('parts.dashboard.chosen.addShort')}
            </Button>
          </li>
        {/each}
      </ul>
    {/if}
  </div>
</div>
