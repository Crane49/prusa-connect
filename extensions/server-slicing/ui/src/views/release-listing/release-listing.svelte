<!--
  The Slicers tab: every PrusaSlicer version the module knows about, one row
  each, with the filters, the search and the order of the app's other
  listings. The filters and the order live in the address; the page size, the
  columns and the view are remembered for the reader, as everywhere else.
-->
<script lang="ts">
  import { formatDateTime } from '@prusa/connect-ui/i18n';
  import {
    Alert,
    AlertDescription,
    AlertTitle,
    Button,
    Card,
    CardContent,
    EmptyState,
    ListingNoMatch,
    type FacetDimension,
    type ListField,
    ListingSearch,
    ListingState,
    type ListingStore,
    ListingToolbar,
    clearFacets,
    Pager,
    viewModes,
  } from '@prusa/connect-ui/primitives';
  import ArrowUpCircleIcon from '@lucide/svelte/icons/circle-arrow-up';
  import CloudOffIcon from '@lucide/svelte/icons/cloud-off';
  import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';

  import type { Slicers, Translate } from '../../api';
  import type { PageAddress } from '../../page-address.svelte';
  import type { ReleaseActs } from '../../release-acts.svelte';
  import {
    compareReleases,
    compareVersions,
    filterReleases,
    type ReleaseFacet,
    releaseFacetCounts,
    releaseFacetValues,
    type ReleaseRow,
    releaseRowsOf,
    type ReleaseSortColumn,
  } from '../../releases';
  import { ReleaseRows } from '../release-rows';
  import { ReleaseTable } from '../release-table';

  let {
    slicers,
    administrator,
    address,
    acts,
    store,
    t,
    onCheck,
    onInstallUpdate,
  }: {
    slicers: Slicers;
    administrator: boolean;
    address: PageAddress;
    acts: ReleaseActs;
    store: ListingStore;
    t: Translate;
    /** Reads the GitHub release list again. */
    onCheck: () => void;
    onInstallUpdate: (version: string) => void;
  } = $props();

  // A function, so the words follow the language the page is read in.
  const fieldsOf = (): ListField[] => [
    { id: 'version', label: t('slicer.column.version'), required: true },
    { id: 'channel', label: t('slicer.column.channel') },
    { id: 'released', label: t('slicer.column.released') },
    { id: 'state', label: t('slicer.column.state') },
    { id: 'size', label: t('slicer.column.size') },
    { id: 'digest', label: t('slicer.column.digest') },
  ];
  const FIELDS = $derived(fieldsOf());

  const filter = $derived(address.state.releases);
  const sort = $derived(address.state.releaseSort);
  const all = $derived(releaseRowsOf(slicers));
  const matching = $derived(filterReleases(all, filter));

  const listing = new ListingState<ReleaseRow>({
    id: 'slicer.releases',
    fields: fieldsOf(),
    views: viewModes(['table', 'rows']),
    initialView: 'table',
    store: {
      read: (key) => store.read(key),
      write: (key, value) => {
        store.write(key, value);
      },
    },
    rows: () => matching,
    compare: (a, b) => compareReleases(a, b, sort),
  });

  const labelOf: Record<ReleaseFacet, (value: string) => string> = {
    channel: (value) => t(`slicer.channel.${value}`),
    state: (value) => t(`slicer.releaseState.${value}`),
    line: (value) => t('slicer.facet.lineValue', { line: value }),
  };

  const facetLabel = $derived<Record<ReleaseFacet, string>>({
    channel: t('slicer.column.channel'),
    state: t('slicer.column.state'),
    line: t('slicer.facet.line'),
  });

  $effect(() => {
    listing.setFacets(
      (['channel', 'state', 'line'] as const).map((id): FacetDimension => ({
        id,
        label: facetLabel[id],
        values: releaseFacetValues(all, id),
        counts: releaseFacetCounts(all, filter, id),
        selected: filter[id],
        onSelectedChange: (next: string[]) => {
          address.update({ releases: { ...filter, [id]: next } });
          listing.setPage(0);
        },
        labelFor: labelOf[id],
      })),
    );
  });

  const shows = (field: string): boolean =>
    listing.fields.visible.includes(field);

  const onSearch = (q: string): void => {
    address.update({ releases: { ...filter, q } });
    listing.setPage(0);
  };

  const onClearAll = (): void => {
    onSearch('');
    clearFacets(listing.facets);
  };

  const onSort = (column: ReleaseSortColumn): void => {
    address.update({
      releaseSort: {
        column,
        ascending: sort.column === column ? !sort.ascending : false,
      },
    });
  };

  const hrefOf = (row: ReleaseRow): string =>
    address.href({ release: row.version });

  const catalogue = $derived(slicers.catalogue);

  // Where the list comes from and which version a job gets, on one line.
  const subtitle = $derived(
    [
      t('slicer.catalogue.for', {
        platform: slicers.platform,
        range: catalogue.supported,
      }),
      catalogue.fetched_at
        ? t('slicer.catalogue.fetched', {
            when: formatDateTime(catalogue.fetched_at),
          })
        : '',
      slicers.pinned
        ? t('slicer.installed.pinned')
        : t('slicer.installed.newest'),
    ]
      .filter((part) => part !== '')
      .join(' · '),
  );
  const update = $derived(slicers.update);

  // The newest version GitHub lists has no build for this platform, so
  // nothing newer than what can be installed is on offer.
  const newestIsUnbuilt = $derived(
    slicers.available.some(
      (unbuilt) =>
        !unbuilt.compatible &&
        slicers.available.every(
          (offer) =>
            !offer.compatible ||
            compareVersions(offer.version, unbuilt.version) < 0,
        ),
    ),
  );
</script>

<section
  class="@container/releases grid grid-cols-1 content-start gap-4"
  aria-labelledby="slicer-releases"
  data-part="releases"
>
  <div class="flex flex-wrap items-center justify-between gap-3">
    <div class="grid min-w-0 grid-cols-1 content-start">
      <h2 id="slicer-releases" class="text-base font-semibold">
        {t('slicer.catalogue.title')}
      </h2>
      <span class="text-sm break-words text-muted-foreground">{subtitle}</span>
    </div>
    {#if administrator && catalogue.github}
      <Button size="sm" variant="secondary" onclick={onCheck}>
        <RefreshCwIcon aria-hidden="true" />
        {t('slicer.action.check')}
      </Button>
    {/if}
  </div>

  {#each slicers.problems as problem (problem.what)}
    <Alert variant="destructive">
      <AlertDescription class="break-words">
        {t('slicer.problem', { what: problem.what, error: problem.error })}
      </AlertDescription>
    </Alert>
  {/each}
  {#if catalogue.error}
    <Alert variant="destructive" data-part="catalogue-error">
      <AlertDescription class="break-words">
        {t('slicer.catalogue.error', { error: catalogue.error })}
      </AlertDescription>
    </Alert>
  {/if}
  {#if all.length > 0 && catalogue.github && newestIsUnbuilt}
    <Alert data-part="catalogue-hint">
      <AlertDescription class="break-words">
        {t('slicer.catalogue.emptyHint')}
      </AlertDescription>
    </Alert>
  {/if}
  {#if update}
    <Alert data-part="update">
      <ArrowUpCircleIcon aria-hidden="true" />
      <AlertTitle
        >{t('slicer.update.title', { version: update.version })}</AlertTitle
      >
      <AlertDescription class="flex flex-wrap items-center gap-3">
        <span
          >{t('slicer.update.description', {
            current: slicers.default ?? '',
          })}</span
        >
        {#if administrator && update.verified}
          <Button size="sm" onclick={() => onInstallUpdate(update.version)}>
            {t('slicer.update.install', { version: update.version })}
          </Button>
        {/if}
      </AlertDescription>
    </Alert>
  {/if}

  <Card>
    <CardContent class="grid grid-cols-1 content-start gap-4">
      {#if all.length === 0}
        <EmptyState
          headingLevel="h3"
          icon={CloudOffIcon}
          heading={t('slicer.catalogue.empty')}
          description={catalogue.github
            ? t('slicer.catalogue.emptyHint')
            : t('slicer.catalogue.off')}
        />
      {:else}
        <ListingToolbar
          facets={listing.facets}
          fields={FIELDS}
          fieldState={listing.fields}
          view={listing.view}
        >
          {#snippet search()}
            <ListingSearch
              value={filter.q}
              onValue={onSearch}
              label={t('slicer.releases.search')}
              placeholder={t('slicer.releases.searchPlaceholder')}
              maxlength={120}
            />
          {/snippet}
        </ListingToolbar>

        {#if listing.total === 0}
          <ListingNoMatch
            heading={t('slicer.releases.filtered')}
            headingLevel="h3"
            description={t('slicer.releases.filteredNote')}
            onClear={onClearAll}
          />
        {:else}
          {#if listing.view.value === 'rows'}
            <ReleaseRows
              rows={listing.page}
              {shows}
              {hrefOf}
              {administrator}
              {acts}
              {t}
            />
          {:else}
            <ReleaseTable
              rows={listing.page}
              {shows}
              {sort}
              {onSort}
              {hrefOf}
              {administrator}
              {acts}
              {t}
            />
          {/if}
          {#if listing.pager}
            <Pager
              {...listing.pager}
              total={listing.total}
              unit={t('slicer.releases.unit', { count: listing.total })}
            />
          {/if}
        {/if}
      {/if}
    </CardContent>
  </Card>
</section>
