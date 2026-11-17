<!--
  The Jobs tab: every job the person may see, newest first, with the filters,
  the search and the order of the app's other listings. The filters and the
  order live in the address.
-->
<script lang="ts">
  import {
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
  import ListChecksIcon from '@lucide/svelte/icons/list-checks';

  import type { Job, JobList, Translate } from '../../api';
  import {
    compareJobs,
    filterJobs,
    type JobFacet,
    jobFacetCounts,
    jobFacetValues,
    type JobSortColumn,
  } from '../../jobs';
  import type { PageAddress } from '../../page-address.svelte';
  import { JobRows } from '../job-rows';
  import { JobTable } from '../job-table';

  let {
    jobs,
    address,
    store,
    t,
    onCancel,
  }: {
    jobs: JobList;
    address: PageAddress;
    store: ListingStore;
    t: Translate;
    onCancel: (id: string) => void;
  } = $props();

  // A function, so the words follow the language the page is read in.
  const fieldsOf = (): ListField[] => [
    { id: 'project', label: t('slicer.column.project'), required: true },
    { id: 'state', label: t('slicer.column.state') },
    { id: 'slicer', label: t('slicer.column.slicer') },
    { id: 'created', label: t('slicer.column.created') },
    { id: 'duration', label: t('slicer.column.duration') },
    { id: 'output', label: t('slicer.column.result') },
    { id: 'by', label: t('slicer.column.by') },
  ];
  const FIELDS = $derived(fieldsOf());

  const filter = $derived(address.state.jobs);
  const sort = $derived(address.state.jobSort);
  const all = $derived(jobs.jobs);
  const matching = $derived(filterJobs(all, filter));

  const listing = new ListingState<Job>({
    id: 'slicer.jobs',
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
    compare: (a, b) => compareJobs(a, b, sort),
  });

  const labelOf: Record<JobFacet, (value: string) => string> = {
    state: (value) => t(`slicer.state.${value}`),
    slicer: (value) => value,
    project: (value) => value,
  };

  const facetLabel = $derived<Record<JobFacet, string>>({
    state: t('slicer.column.state'),
    slicer: t('slicer.column.slicer'),
    project: t('slicer.column.project'),
  });

  $effect(() => {
    listing.setFacets(
      (['state', 'slicer', 'project'] as const).map((id): FacetDimension => ({
        id,
        label: facetLabel[id],
        values: jobFacetValues(all, id),
        counts: jobFacetCounts(all, filter, id),
        selected: filter[id],
        onSelectedChange: (next: string[]) => {
          address.update({ jobs: { ...filter, [id]: next } });
          listing.setPage(0);
        },
        labelFor: labelOf[id],
      })),
    );
  });

  const shows = (field: string): boolean =>
    listing.fields.visible.includes(field);

  const onSearch = (q: string): void => {
    address.update({ jobs: { ...filter, q } });
    listing.setPage(0);
  };

  const onClearAll = (): void => {
    onSearch('');
    clearFacets(listing.facets);
  };

  const onSort = (column: JobSortColumn): void => {
    address.update({
      jobSort: {
        column,
        ascending: sort.column === column ? !sort.ascending : false,
      },
    });
  };

  const hrefOf = (job: Job): string => address.href({ job: job.id });
</script>

<section
  class="@container/jobs grid grid-cols-1 content-start gap-4"
  aria-labelledby="slicer-jobs"
  data-part="jobs"
>
  <h2 id="slicer-jobs" class="sr-only">
    {jobs.all ? t('slicer.jobs.titleAll') : t('slicer.jobs.title')}
  </h2>
  <Card>
    <CardContent class="grid grid-cols-1 content-start gap-4">
      {#if all.length === 0}
        <EmptyState
          headingLevel="h3"
          icon={ListChecksIcon}
          heading={t('slicer.jobs.empty')}
          description={t('slicer.jobs.emptyHint')}
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
              label={t('slicer.jobs.search')}
              placeholder={t('slicer.jobs.searchPlaceholder')}
              maxlength={120}
            />
          {/snippet}
        </ListingToolbar>

        {#if listing.total === 0}
          <ListingNoMatch
            heading={t('slicer.jobs.filtered')}
            headingLevel="h3"
            description={t('slicer.jobs.filteredNote')}
            onClear={onClearAll}
          />
        {:else}
          {#if listing.view.value === 'rows'}
            <JobRows
              jobs={listing.page}
              all={jobs.all}
              {shows}
              {hrefOf}
              {onCancel}
              {t}
            />
          {:else}
            <JobTable
              jobs={listing.page}
              all={jobs.all}
              {shows}
              {sort}
              {onSort}
              {hrefOf}
              {onCancel}
              {t}
            />
          {/if}
          {#if listing.pager}
            <Pager
              {...listing.pager}
              total={listing.total}
              unit={t('slicer.jobs.unit', { count: listing.total })}
            />
          {/if}
        {/if}
      {/if}
    </CardContent>
  </Card>
</section>
