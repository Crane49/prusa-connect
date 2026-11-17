<!--
  The folder on screen: the trail up to it below the top, the folders inside it, and one page
  of its parts, as cards or as a table.

  A search or a tag spans the folder on screen and every folder under it, so
  while one is on the folders inside are not drawn: the parts that matched are
  the answer, from wherever they are.

  Each emptiness says which one it is. A catalogue with no part asks for the
  first one, a folder with nothing in it says so, and a search that matched
  nothing names the search. A listing that could not be read is drawn by its
  code.
-->
<script lang="ts">
  import FolderIcon from '@lucide/svelte/icons/folder';
  import InboxIcon from '@lucide/svelte/icons/inbox';
  import PackageIcon from '@lucide/svelte/icons/package';
  import PencilLineIcon from '@lucide/svelte/icons/pencil-line';
  import PlusIcon from '@lucide/svelte/icons/plus';
  import Trash2Icon from '@lucide/svelte/icons/trash-2';
  import {
    Button,
    DropdownMenuItem,
    EmptyState,
    ListingNoMatch,
    FaultAlert,
    FolderTrail,
    ListingSearch,
    ListingSearchSummary,
    ListingToolbar,
    ListingSkeleton,
    Pager,
    RowActions,
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
    Tile,
    type TilePressProps,
    viewModes,
    type PageSize,
  } from '@prusa/connect-ui/primitives';
  import { onDestroy } from 'svelte';

  import type { Order } from '../catalogue.svelte';
  import { faultText } from '../faults';
  import { MAX_NAME } from '../presentation';
  import { TOP } from '../navigation.svelte';
  import type { Folder } from '../parts';
  import { heldSession } from '../session';
  import { CARD_GRID } from './layout';
  import FolderLink from './folder-link.svelte';
  import PartCards from './part-cards.svelte';
  import PartTable from './part-table.svelte';

  let {
    onNewPart,
    onChangeFolder,
    onDeleteFolder,
  }: {
    onNewPart: () => void;
    onChangeFolder: (folder: Folder) => void;
    onDeleteFolder: (folder: Folder) => void;
  } = $props();

  const { catalogue, navigation, host } = heldSession();
  const t = host.t;

  /** The key the view the person chose is kept under, for them alone. */
  const VIEW_KEY = 'view';

  let view = $state(host.store.get(VIEW_KEY) === 'table' ? 'table' : 'cards');
  // The search box shows the person's draft while they type, and what the
  // address searched for otherwise, so the back button puts the old search
  // back into the box.
  let draft = $state<string | undefined>(undefined);
  let timer: ReturnType<typeof setTimeout> | undefined;

  /** How long the box waits after the last key before it searches. */
  const SETTLE_MS = 300;

  const spot = $derived(navigation.spot);
  const typed = $derived(draft ?? spot.q);
  const here = $derived(catalogue.folder(spot.folder));
  const searching = $derived(navigation.searching);
  const folders = $derived(searching ? [] : catalogue.childrenOf(spot.folder));
  const mayWrite = $derived(catalogue.allowed.write);
  // The module deletes only an empty folder. The overview counts what each
  // folder holds directly, so the menu says so up front rather than offering
  // a delete the module would refuse.
  const hereEmpty = $derived(
    here !== undefined && (here.parts ?? 0) === 0 && (here.folders ?? 0) === 0,
  );

  const crumbs = $derived([
    { label: t('parts.tree.all'), path: '0' },
    ...catalogue
      .trailOf(spot.folder)
      .map((folder) => ({ label: folder.name, path: String(folder.id) })),
  ]);

  const fault = $derived(
    catalogue.listingFault === undefined
      ? undefined
      : faultText(catalogue.listingFault, t),
  );

  // A search can be words, a tag, or both, and the summary names each one the
  // person asked for.
  const summary = $derived.by(() => {
    const count = catalogue.matched;
    if (spot.tag === '') {
      return t('parts.search.results', { count, query: spot.q });
    }
    if (spot.q === '') {
      return t('parts.search.tagResults', { count, tag: spot.tag });
    }

    return t('parts.search.tagQueryResults', {
      count,
      tag: spot.tag,
      query: spot.q,
    });
  });

  const views = $derived(viewModes(['table', 'cards']));
  const orders = $derived([
    { value: 'name', label: t('parts.order.name') },
    { value: 'changed', label: t('parts.order.changed') },
  ]);

  const pageCount = $derived(
    Math.max(1, Math.ceil(catalogue.matched / catalogue.limit)),
  );

  const nothingHere = $derived(
    catalogue.listed && catalogue.rows.length === 0 && folders.length === 0,
  );

  const onSearch = (next: string): void => {
    draft = next;
    clearTimeout(timer);
    timer = setTimeout(() => {
      navigation.search(next.trim(), spot.tag);
      draft = undefined;
    }, SETTLE_MS);
  };

  const clearSearch = (): void => {
    clearTimeout(timer);
    draft = undefined;
    navigation.search('', '');
  };

  // A search the person typed and then left the page before it settled is
  // not sent after they have gone.
  onDestroy(() => {
    clearTimeout(timer);
  });

  const chooseView = (next: string): void => {
    view = next === 'table' ? 'table' : 'cards';
    host.store.set(VIEW_KEY, view);
  };
</script>

{#snippet search()}
  <ListingSearch
    value={typed}
    onValue={onSearch}
    maxlength={MAX_NAME}
    label={t('parts.search.label')}
    placeholder={spot.folder === 0
      ? t('parts.search.placeholder')
      : t('parts.search.placeholderFolder', { name: here?.name ?? '' })}
  />
{/snippet}

{#snippet beforeControls()}
  <Select
    value={catalogue.order}
    items={orders}
    onValueChange={(next: string) => {
      catalogue.order = next as Order;
    }}
  >
    <SelectTrigger size="sm" class="w-fit" aria-label={t('parts.order.label')}>
      <SelectValue />
    </SelectTrigger>
    <SelectContent aria-label={t('parts.order.label')}>
      {#each orders as order (order.value)}
        <SelectItem value={order.value} label={order.label}
          >{order.label}</SelectItem
        >
      {/each}
    </SelectContent>
  </Select>
{/snippet}

<!-- At the top the trail would only repeat the tree's first row. -->
{#if crumbs.length > 1}
  <div class="flex flex-wrap items-center justify-between gap-2">
    <FolderTrail
      {crumbs}
      label={t('parts.trail.label')}
      onOpen={(path: string) => {
        navigation.openFolder(Number(path));
      }}
    />
    {#if here && mayWrite && !searching}
      <RowActions name={here.name}>
        <DropdownMenuItem
          onSelect={() => {
            onChangeFolder(here);
          }}
        >
          <PencilLineIcon aria-hidden="true" />
          {t('parts.folder.edit')}
        </DropdownMenuItem>
        <DropdownMenuItem
          variant="destructive"
          disabled={!hereEmpty}
          data-part="delete-folder"
          onSelect={() => {
            if (hereEmpty) onDeleteFolder(here);
          }}
        >
          <Trash2Icon aria-hidden="true" />
          <span class="grid">
            {t('parts.folder.delete.action')}
            {#if !hereEmpty}
              <span class="text-xs text-muted-foreground" data-part="not-empty">
                {t('parts.folder.delete.notEmpty')}
              </span>
            {/if}
          </span>
        </DropdownMenuItem>
      </RowActions>
    {/if}
  </div>
{/if}

{#if here?.description && !searching}
  <p
    class="text-sm whitespace-pre-line text-muted-foreground"
    data-part="folder-description"
  >
    {here.description}
  </p>
{/if}

<ListingToolbar
  {search}
  {beforeControls}
  view={{ value: view, onValueChange: chooseView, options: views }}
/>

{#if searching}
  <ListingSearchSummary
    class="justify-between"
    {summary}
    clearLabel={t('parts.search.clear')}
    onClear={clearSearch}
  />
{/if}

<div data-part="listing" class="grid gap-y-4" aria-busy={catalogue.listing}>
  {#if fault}
    <FaultAlert {fault} />
  {:else if !catalogue.listed}
    <ListingSkeleton label={t('parts.listing.loading')} art rows={6} />
  {:else if nothingHere}
    {#if searching}
      <ListingNoMatch
        headingLevel="h2"
        heading={t('parts.empty.search.heading')}
        description={t('parts.empty.search.note')}
        clearLabel={t('parts.search.clear')}
        onClear={clearSearch}
      />
    {:else if catalogue.held === 0}
      <EmptyState
        icon={PackageIcon}
        headingLevel="h2"
        heading={t('parts.empty.catalogue.heading')}
        description={mayWrite
          ? t('parts.empty.catalogue.note')
          : t('parts.empty.catalogue.viewer')}
      >
        {#snippet action()}
          {#if mayWrite}
            <Button size="md" onclick={onNewPart}>
              <PlusIcon aria-hidden="true" />
              {t('parts.action.newPart')}
            </Button>
          {/if}
        {/snippet}
      </EmptyState>
    {:else}
      <EmptyState
        icon={InboxIcon}
        headingLevel="h2"
        heading={t('parts.empty.folder.heading')}
        description={t('parts.empty.folder.note')}
      />
    {/if}
  {:else}
    {#if folders.length > 0}
      <ul class={CARD_GRID} aria-label={t('parts.listing.folders')}>
        {#each folders as folder (folder.id)}
          <li>
            <Tile>
              {#snippet child({ props }: { props: TilePressProps })}
                <FolderLink
                  {...props}
                  spot={{ ...TOP, folder: folder.id }}
                  class={[props.class, 'flex items-center gap-3']}
                >
                  <FolderIcon
                    aria-hidden="true"
                    class="size-5 shrink-0 text-muted-foreground"
                  />
                  <span class="grid min-w-0">
                    <span class="truncate text-sm font-medium"
                      >{folder.name}</span
                    >
                    <span class="text-xs text-muted-foreground">
                      {t('parts.folder.holds', {
                        parts: folder.parts ?? 0,
                        folders: folder.folders ?? 0,
                      })}
                    </span>
                  </span>
                </FolderLink>
              {/snippet}
            </Tile>
          </li>
        {/each}
      </ul>
    {/if}

    {#if catalogue.rows.length > 0}
      {#if view === 'table'}
        <PartTable />
      {:else}
        <PartCards />
      {/if}
    {/if}

    {#if catalogue.matched > catalogue.limit}
      <Pager
        page={Math.floor(catalogue.offset / catalogue.limit)}
        {pageCount}
        pageSize={catalogue.limit}
        total={catalogue.matched}
        unit={t('parts.listing.unit', { count: catalogue.matched })}
        onPageChange={(next: number) => {
          catalogue.offset = next * catalogue.limit;
        }}
        onPageSizeChange={(next: PageSize) => {
          catalogue.limit = next;
        }}
      />
    {/if}
  {/if}
</div>
