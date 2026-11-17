<!-- This screen lists the printers, however many this server holds. -->
<script lang="ts">
  import ArchiveIcon from '@lucide/svelte/icons/archive';
  import ArrowDownWideNarrowIcon from '@lucide/svelte/icons/arrow-down-wide-narrow';
  import ArrowUpNarrowWideIcon from '@lucide/svelte/icons/arrow-up-narrow-wide';
  import InboxIcon from '@lucide/svelte/icons/inbox';
  import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';
  import PlusIcon from '@lucide/svelte/icons/plus';
  import { untrack } from 'svelte';

  import {
    homeShapeFor,
    PRINTER_STATE_LABEL_KEY,
    printerStateOf,
    tagKey,
  } from '../../domain';
  import { t } from '../../i18n';
  import {
    Button,
    Card,
    CardContent,
    CardHeader,
    clearFacets,
    EmptyState,
    type FacetDimension,
    type ListField,
    ListingNoMatch,
    ListingSearch,
    ListingState,
    ListingToolbar,
    PageHeader,
    Pager,
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
    Skeleton,
    Toggle,
    viewModes,
  } from '../../primitives';
  import FleetIssueSummary from './fleet-issue-summary.svelte';
  import { browserStore, printerRowOf } from './presentation';
  import PrinterCard from './printer-card.svelte';
  import PrinterFrame from './printer-frame.svelte';
  import PrintersListing from './printers-listing.svelte';
  import type {
    FleetListingSource,
    PrinterFieldId,
    PrinterRow,
    PrintersScreenProps,
  } from './types';

  let {
    listing,
    printerCount,
    tags,
    cameras,
    preference = 'all-printers',
    onAdd,
    actions,
    store = browserStore(),
    cardLine,
    issues,
    nearby,
  }: PrintersScreenProps = $props();

  type SortField = FleetListingSource['sorts'][number]['field'];

  const GRID = 'grid auto-rows-fr grid-cols-printers gap-6';

  const FRAME_GRID = 'grid auto-rows-fr grid-cols-frames gap-6';

  const SEARCH_DEBOUNCE_MS = 250;

  const FIELDS: ListField[] = [
    { id: 'printer', label: t('printers.printer'), required: true },
    { id: 'state', label: t('printers.state') },
    { id: 'job', label: t('printers.job') },
    { id: 'progress', label: t('printers.progress') },
    { id: 'nozzle', label: t('printers.nozzle') },
    { id: 'bed', label: t('printers.bed') },
    { id: 'material', label: t('printers.material') },
    { id: 'issue', label: t('printers.issue') },
    { id: 'actions', label: t('printers.actions'), required: true },
  ];

  const SORT_LABEL_KEY: Record<SortField, string> = {
    urgency: 'printers.sort.urgency',
    name: 'printers.sort.name',
    model: 'printers.sort.model',
    state: 'printers.sort.state',
    last_seen: 'printers.sort.last_seen',
    id: 'printers.sort.id',
  };

  const ORDER_LABEL_KEY = {
    asc: 'printers.sort.asc',
    desc: 'printers.sort.desc',
  } as const;

  // The strip owns severity, so it has no facet here.
  const FACET_LABEL_KEY: Partial<
    Record<FleetListingSource['filters'][number]['param'], string>
  > = {
    state: 'printers.state',
    model: 'printers.type',
    tag: 'printers.tags',
  };

  const listingState = new ListingState<PrinterRow>({
    id: 'printers',
    fields: FIELDS,
    views: viewModes(['table', 'cards', 'frames']),
    initialView: 'cards',
    store: {
      read: (key) => store.read(key),
      write: (key, value) => store.write(key, value),
    },
    rows: () =>
      listing.items.map((device) =>
        printerRowOf(
          device,
          issues && ((reason: string) => issues.entryOf(reason)),
        ),
      ),
    total: () => listing.total,
    shape: () =>
      homeShapeFor({ printerCount, preference }) === 'fleet' ? 'fleet' : 'hero',
  });

  // Keyed by the filter's query parameter. The tags are the host's, so they
  // are not held here.
  let selection = $state.raw<Record<string, string[]>>({});
  let retired = $state(false);
  let typed = $state('');
  let search = $state('');
  let typing: ReturnType<typeof setTimeout> | undefined;

  const chosenTags = $derived(tags?.chosen ?? []);
  const chosenKeys = $derived(new Set(chosenTags.map(tagKey)));

  const filters = $derived<Record<string, readonly string[]>>(
    tags === undefined ? selection : { ...selection, tag: chosenTags },
  );

  // Before the first answer nothing is advertised and the listing checks the
  // field itself; after it, a remembered field the server dropped is not sent.
  const sort = $derived.by(() => {
    const held = listingState.sort;
    if (held === undefined) return undefined;
    const offered =
      listing.sorts.length === 0 ||
      listing.sorts.some((one) => one.field === held.field);

    return offered ? held : undefined;
  });
  const sortField = $derived(sort?.field);
  const sortOrder = $derived(sort?.order);
  const pageSize = $derived(listingState.pager?.pageSize ?? 0);

  $effect(() => {
    const query = {
      page: listingState.pageIndex,
      pageSize,
      filters,
      search,
      sort: sortField,
      order: sortOrder,
      retired: retired ? ('only' as const) : ('exclude' as const),
    };
    // `setQuery` reads the listing's own state; this effect follows only the query.
    untrack(() => {
      listing.setQuery(query);
    });
  });

  // The listing steps back a page when printers left past the one on screen.
  $effect(() => {
    const at = listing.query.page;
    untrack(() => {
      if (at !== listingState.pageIndex) listingState.setPage(at);
    });
  });

  $effect(() => () => {
    clearTimeout(typing);
  });

  const choose = (param: string, next: string[]): void => {
    if (param === 'tag') {
      tags?.onChange(next);
    } else {
      selection = { ...selection, [param]: next };
    }
    listingState.setPage(0);
  };

  const typeSearch = (next: string): void => {
    typed = next;
    clearTimeout(typing);
    typing = setTimeout(() => {
      search = next;
      listingState.setPage(0);
    }, SEARCH_DEBOUNCE_MS);
  };

  const filtersActive = $derived(
    chosenTags.length > 0 ||
      (selection.severity?.length ?? 0) > 0 ||
      listingState.facets.some((facet) => facet.selected.length > 0),
  );

  // The host holds the chosen tags, and the hero shape draws no tag facet,
  // so the tags are let go once here and never through a facet. The held
  // selection also covers the severity strip and facets of another shape.
  const clearFilters = (): void => {
    selection = {};
    clearFacets(listingState.facets.filter((facet) => facet.id !== 'tag'));
    if (chosenTags.length > 0) choose('tag', []);
  };

  const clearSearchAndFilters = (): void => {
    typeSearch('');
    clearFilters();
  };

  const sortBy = (field: string, order: 'asc' | 'desc'): void => {
    listingState.setSort(field, order);
    listingState.setPage(0);
  };

  const labelFor = (param: string): ((value: string) => string) => {
    if (param === 'state') {
      return (value) => t(PRINTER_STATE_LABEL_KEY[printerStateOf(value)]);
    }

    return (value) => value;
  };

  // A tag spelled another way in the address is the tag the server lists.
  const selectedOf = (param: string, values: readonly string[]): string[] =>
    param === 'tag'
      ? values.filter((value) => chosenKeys.has(tagKey(value)))
      : (selection[param] ?? []);

  const searchFilter = $derived(
    listing.filters.find((one) => one.param === 'search'),
  );

  // The tag filter needs a host that keeps the chosen tags.
  $effect(() => {
    listingState.setFacets(
      listing.filters.flatMap((one): FacetDimension[] => {
        const label = FACET_LABEL_KEY[one.param];
        if (one.kind !== 'multi' || label === undefined) return [];
        if (one.param === 'tag' && tags === undefined) return [];
        const values = (one.values ?? []).map((entry) => entry.value);

        return [
          {
            id: one.param,
            label: t(label),
            values,
            counts: new Map(
              (one.values ?? []).map((entry) => [entry.value, entry.count]),
            ),
            selected: selectedOf(one.param, values),
            onSelectedChange: (next: string[]) => {
              choose(one.param, next);
            },
            labelFor: labelFor(one.param),
          },
        ];
      }),
    );
  });

  // The field and direction the server ordered this page by, until the reader
  // picks one.
  const shownField = $derived(sort?.field ?? listing.sort);
  const shownOrder = $derived(sort?.order ?? listing.order ?? 'asc');

  const shows = (field: PrinterFieldId): boolean =>
    listingState.fields.visible.includes(field);

  const fleetIsEmpty = $derived(
    printerCount === 0 && listing.total === 0 && !retired,
  );
</script>

{#snippet addButton()}
  <Button
    size="md"
    onclick={() => {
      onAdd();
    }}
  >
    <PlusIcon aria-hidden="true" />
    {t('printers.add')}
  </Button>
{/snippet}

{#snippet retiredToggle()}
  <Toggle
    pressed={retired}
    onPressedChange={(next: boolean) => {
      retired = next;
      listingState.setPage(0);
    }}
  >
    <ArchiveIcon aria-hidden="true" />
    {t('printers.retired.only')}
  </Toggle>
{/snippet}

{#snippet headerActions()}
  {@render retiredToggle()}
  <!-- Hidden rather than left out, so the toggle stays under the pointer
       that pressed it, and a second press cannot land on add instead. -->
  <div class={retired ? 'invisible' : undefined}>
    {@render addButton()}
  </div>
{/snippet}

{#snippet cards(layout: string)}
  <div class={layout}>
    {#each listingState.page as row (row.id)}
      <PrinterCard
        {row}
        {shows}
        {...actions === undefined ? {} : { actions }}
        {...cardLine === undefined ? {} : { line: cardLine }}
      />
    {/each}
  </div>
{/snippet}

{#snippet issueStrip()}
  <FleetIssueSummary
    counts={listing.issues?.by_severity}
    selected={selection.severity ?? []}
    onSelectedChange={(next: string[]) => {
      choose('severity', next);
    }}
  />
{/snippet}

{#snippet searchBox()}
  <ListingSearch
    value={typed}
    onValue={typeSearch}
    label={t('printers.search')}
    placeholder={t('printers.search')}
    maxlength={searchFilter?.max_length}
  />
{/snippet}

{#snippet beforeControls()}
  {@render issueStrip()}

  {#if listing.sorts.length > 0}
    <div class="flex items-center gap-1">
      <Select
        value={shownField ?? ''}
        onValueChange={(field: string) => {
          const offered = listing.sorts.find((one) => one.field === field);
          if (offered !== undefined) sortBy(field, offered.default_order);
        }}
        items={listing.sorts.map((one) => ({
          value: one.field,
          label: t(SORT_LABEL_KEY[one.field]),
        }))}
      >
        <SelectTrigger
          size="sm"
          aria-label={t('printers.sort.label')}
          class="w-fit"
        >
          <SelectValue />
        </SelectTrigger>
        <SelectContent aria-label={t('printers.sort.label')}>
          {#each listing.sorts as one (one.field)}
            <SelectItem value={one.field}>
              {t(SORT_LABEL_KEY[one.field])}
            </SelectItem>
          {/each}
        </SelectContent>
      </Select>

      {#if shownField !== undefined}
        <Button
          variant="ghost"
          size="sm"
          aria-label={t(ORDER_LABEL_KEY[shownOrder])}
          onclick={() => {
            sortBy(shownField, shownOrder === 'asc' ? 'desc' : 'asc');
          }}
        >
          {#if shownOrder === 'asc'}
            <ArrowUpNarrowWideIcon aria-hidden="true" />
          {:else}
            <ArrowDownWideNarrowIcon aria-hidden="true" />
          {/if}
        </Button>
      {/if}
    </div>
  {/if}
{/snippet}

<div class="flex flex-col gap-4">
  <!-- An empty fleet offers the add act in its empty state, but the retired
       printers stay reachable. The retired list offers no add, because a
       printer added there would land in the other list, and its heading says
       which list is on screen: the cards of both look alike. The header keeps
       the height of the large add button, so the page does not move when the
       fleet arrives. -->
  <PageHeader
    class="min-h-11"
    title={retired ? t('printers.retired.only') : t('printers.title')}
    actions={fleetIsEmpty ? retiredToggle : headerActions}
  />

  {#if listing.loading && !listing.loaded}
    <div role="status" aria-label={t('printers.loading')} class={GRID}>
      {#each { length: 6 }, card (card)}
        <Card>
          <CardHeader>
            <Skeleton class="h-4 w-40" />
            <Skeleton class="h-3 w-20" />
          </CardHeader>
          <CardContent class="flex flex-col gap-4">
            <Skeleton class="h-10 w-full" />
            <Skeleton class="h-8 w-full" />
          </CardContent>
        </Card>
      {/each}
    </div>
  {:else if listing.error !== undefined}
    <Card>
      <CardContent>
        <EmptyState
          icon={TriangleAlertIcon}
          heading={t('printers.unreadable.heading')}
          headingLevel="h2"
          description={t('printers.unreadable.note')}
        />
      </CardContent>
    </Card>
  {:else if fleetIsEmpty}
    <Card>
      <CardContent>
        <EmptyState
          icon={InboxIcon}
          heading={t('printers.empty.heading')}
          headingLevel="h2"
          description={t('printers.empty.note')}
          action={addButton}
        />
      </CardContent>
    </Card>
  {:else}
    {#if listingState.shape !== 'fleet'}
      {@render issueStrip()}
      {#if filtersActive}
        <ListingToolbar
          facets={[]}
          {filtersActive}
          onClearFilters={clearFilters}
        />
      {/if}
    {:else}
      <ListingToolbar
        search={searchFilter === undefined ? undefined : searchBox}
        {beforeControls}
        facets={listingState.facets}
        fields={FIELDS}
        fieldState={listingState.fields}
        view={listingState.view}
        {filtersActive}
        onClearFilters={clearFilters}
      />
    {/if}

    {#if listing.total === 0}
      <Card>
        <CardContent>
          {#if retired}
            <EmptyState
              icon={InboxIcon}
              heading={t('printers.retired.empty.heading')}
              headingLevel="h2"
              description={t('printers.retired.empty.note')}
            />
          {:else}
            <ListingNoMatch
              heading={t('printers.filtered.heading')}
              headingLevel="h2"
              description={t('printers.filtered.note')}
              onClear={clearSearchAndFilters}
            />
          {/if}
        </CardContent>
      </Card>
    {:else if listingState.shape === 'hero'}
      {@render cards('flex flex-col gap-6')}
    {:else if listingState.view.value === 'table'}
      <PrintersListing rows={listingState.page} {shows} />
    {:else if listingState.view.value === 'frames'}
      <div class={FRAME_GRID}>
        {#each listingState.page as row (row.id)}
          <PrinterFrame {row} camera={cameras?.feedFor(row.id)} />
        {/each}
      </div>
    {:else}
      {@render cards(GRID)}
    {/if}

    {#if listingState.pager}
      <Pager {...listingState.pager} total={listing.total} />
    {/if}
  {/if}

  {@render nearby?.()}
</div>
