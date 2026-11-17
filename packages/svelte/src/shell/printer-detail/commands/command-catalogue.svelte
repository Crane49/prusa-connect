<!-- Every verb this printer accepts, refused ones included. -->
<script lang="ts">
  import SearchXIcon from '@lucide/svelte/icons/search-x';
  import SlidersHorizontalIcon from '@lucide/svelte/icons/sliders-horizontal';

  import { t } from '../../../i18n';
  import {
    Card,
    CardContent,
    CardHeader,
    CardTitle,
    EmptyState,
    Field,
    Input,
    RowList,
  } from '../../../primitives';
  import CommandRow from './command-row.svelte';
  import type { CommandCatalogueProps, CommandListing } from './types';
  import {
    COMMAND_GROUPS,
    GROUP_LABEL_KEY,
    groupOf,
    labelKeyOf,
    rankOf,
    spellOut,
  } from './vocabulary';

  let {
    rows,
    marked,
    sending,
    pending,
    readingDrive,
    onSend,
  }: CommandCatalogueProps = $props();

  let query = $state('');

  const nameOf = (listing: CommandListing): string => {
    const key = labelKeyOf(listing.entry.verb);

    return key === undefined ? spellOut(listing.entry.verb) : t(key);
  };

  const trimmed = $derived(query.trim());

  // Searches the name a person reads as well as the wire token.
  const visible = $derived(
    trimmed === ''
      ? rows
      : rows.filter((listing) =>
          `${nameOf(listing)} ${listing.entry.verb}`
            .toLowerCase()
            .includes(trimmed.toLowerCase()),
        ),
  );

  const groups = $derived(
    COMMAND_GROUPS.map((group) => ({
      group,
      listings: visible
        .filter((listing) => groupOf(listing.entry.verb) === group)
        .sort(
          (one, other) => rankOf(one.entry.verb) - rankOf(other.entry.verb),
        ),
    })).filter((section) => section.listings.length > 0),
  );
</script>

<Card>
  <CardHeader>
    <CardTitle>
      <h2 class="contents">{t('printerDetailCommands.catalogue.heading')}</h2>
    </CardTitle>
  </CardHeader>
  <CardContent class="flex flex-col gap-4">
    {#if rows.length === 0}
      <EmptyState
        icon={SlidersHorizontalIcon}
        heading={t('printerDetailCommands.none.heading')}
        headingLevel="h3"
        description={t('printerDetailCommands.none.detail')}
      />
    {:else}
      <Field
        class="max-w-sm"
        label={t('printerDetail.commands.search.label')}
        control={Input}
        controlProps={{
          type: 'text',
          placeholder: t('printerDetail.commands.search.placeholder'),
          value: query,
          oninput: (event: Event) => {
            const field = event.currentTarget;
            if (field instanceof HTMLInputElement) query = field.value;
          },
        }}
      />
      <!-- The filtered list is feedback enough for a sighted reader. -->
      <div aria-live="polite" class="sr-only">
        {trimmed === ''
          ? t('printerDetail.commands.search.available', { count: rows.length })
          : t('printerDetail.commands.search.matching', {
              count: visible.length,
              query: trimmed,
              total: rows.length,
            })}
      </div>

      {#if visible.length === 0}
        <EmptyState
          icon={SearchXIcon}
          heading={t('printerDetail.commands.noMatch.heading')}
          headingLevel="h3"
          description={t('printerDetail.commands.noMatch.detail', {
            query: trimmed,
          })}
        />
      {:else}
        <div class="flex flex-col gap-4">
          {#each groups as section (section.group)}
            <div class="flex flex-col gap-1">
              <!-- The heading sits on the card. -->
              <span class="text-xs font-medium text-muted-foreground">
                {t(GROUP_LABEL_KEY[section.group])}
              </span>
              <RowList>
                {#each section.listings as listing (listing.entry.verb)}
                  <CommandRow
                    entry={listing.entry}
                    projection={listing.projection}
                    marked={marked === listing.entry.verb}
                    {sending}
                    {pending}
                    {readingDrive}
                    {onSend}
                  />
                {/each}
              </RowList>
            </div>
          {/each}
        </div>
      {/if}
    {/if}
  </CardContent>
</Card>
