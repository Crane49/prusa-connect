<!--
  The folders other Fractal servers share with this one, under this server's
  own Storage. At the top it lists the top of every shared storage, grouped by
  server in the order of the tree; a folder opens in place.

  Every row says which server, storage and folder it is in (R15). A listing
  from another server carries no date (R16) and no size of the storage (R4).
  A server whose report may be out of date is greyed out with the time it was
  last heard from (R17).
-->
<script lang="ts">
  import FileCode2Icon from '@lucide/svelte/icons/file-code-2';
  import FileIcon from '@lucide/svelte/icons/file';
  import FolderIcon from '@lucide/svelte/icons/folder';
  import Share2Icon from '@lucide/svelte/icons/share-2';
  import { isPrintFile } from '@prusa/connect-ui/domain';
  import {
    describeFault,
    formatBytes,
    formatDateTime,
    translateFor,
  } from '@prusa/connect-ui/i18n';
  import {
    Button,
    Card,
    CardContent,
    CardDescription,
    CardHeader,
    CardTitle,
    ClippedName,
    EmptyState,
    FaultAlert,
    FileThumbnail,
    FolderTrail,
    ListingSkeleton,
    Select,
    SelectContent,
    SelectItem,
    SelectTrigger,
    SelectValue,
    Table,
    TableBody,
    TableCaption,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
  } from '@prusa/connect-ui/primitives';
  import { onMount } from 'svelte';

  import { APP_NAMESPACE } from '../../i18n';
  import {
    type FractalStores,
    placeOfFolder,
    type SharedEntry,
    type SharedPlace,
    type SharedStore,
  } from '../../stores/fractal-stores.svelte';

  let {
    stores,
    onNavigate,
  }: {
    stores: FractalStores;
    /**
     * Called with the folder the reader asks for, and undefined for the top,
     * so the page can write it into the address. Without it the list moves
     * by itself and the back button does not follow.
     */
    onNavigate?: (place: SharedPlace | undefined) => void;
  } = $props();

  const t = translateFor(APP_NAMESPACE);

  // No node path holds a star, so it names every server.
  const EVERY = '*';

  // The server the top of the list is narrowed to, and every server for none.
  let only = $state(EVERY);

  onMount(() => {
    void stores.load();
  });

  const nodes = $derived([...new Set(stores.stores.map((one) => one.node))]);
  const atTop = $derived(stores.trail.length === 0);

  // The order of the tree first, as the module sends the stores, then the
  // folders before the files, then by name.
  const rows = $derived(
    stores.rows
      .filter((row) => !atTop || only === EVERY || row.store.node === only)
      .sort(
        (a, b) =>
          nodes.indexOf(a.store.node) - nodes.indexOf(b.store.node) ||
          Number(b.folder) - Number(a.folder) ||
          a.name.localeCompare(b.name),
      ),
  );

  // The stores whose server has not finished its first scan of them.
  const unscanned = $derived(
    stores.stores.filter(
      (one) => one.root === undefined && (only === EVERY || one.node === only),
    ),
  );

  const fault = $derived(
    stores.error === undefined ? undefined : describeFault(stores.error),
  );

  const placeOf = (store: SharedStore): string =>
    `${store.node} · ${store.instance}`;

  const folderOf = (row: SharedEntry): string => {
    const cut = row.path.lastIndexOf('/');

    return cut === -1 ? '/' : `/${row.path.slice(0, cut)}`;
  };

  const walk = (place: SharedPlace | undefined): void => {
    if (onNavigate === undefined) {
      void stores.goTo(place);
    } else {
      onNavigate(place);
    }
  };

  // A crumb's path is how deep it is: 0 for the top, 1 for the first folder.
  const crumbs = $derived([
    { label: t('app.fractal.shared.top'), path: '0' },
    ...stores.trail.map((folder, index) => ({
      label:
        index === 0 ? `${placeOf(folder.store)} · ${folder.name}` : folder.name,
      path: String(index + 1),
    })),
  ]);

  const glyphOf = (row: SharedEntry) => {
    if (row.folder) return FolderIcon;

    return isPrintFile(row.name) ? FileCode2Icon : FileIcon;
  };
</script>

<Card data-part="shared-stores">
  <CardHeader>
    <CardTitle>
      <h2 class="contents">{t('app.fractal.shared.title')}</h2>
    </CardTitle>
    <CardDescription>
      {stores.node === ''
        ? t('app.fractal.shared.note')
        : t('app.fractal.shared.noteAt', { node: stores.node })}
    </CardDescription>
  </CardHeader>
  <CardContent class="flex flex-col gap-4">
    {#if !stores.loaded}
      <ListingSkeleton label={t('app.fractal.shared.loading')} art rows={3} />
    {:else if stores.stores.length === 0 && fault !== undefined}
      <FaultAlert {fault} />
    {:else if stores.stores.length === 0}
      <EmptyState
        icon={Share2Icon}
        heading={t('app.fractal.shared.empty.heading')}
        headingLevel="h3"
        description={t('app.fractal.shared.empty.note')}
      />
    {:else}
      <div class="flex flex-wrap items-center gap-2">
        <FolderTrail
          {crumbs}
          label={t('app.fractal.shared.trail')}
          onOpen={(depth: string) => {
            const folder = stores.trail[Number(depth) - 1];
            walk(folder === undefined ? undefined : placeOfFolder(folder));
          }}
        />
        {#if atTop && nodes.length > 1}
          <div class="ms-auto">
            <Select
              value={only}
              onValueChange={(next: string) => {
                only = next;
              }}
              items={[
                { value: EVERY, label: t('app.fractal.shared.top') },
                ...nodes.map((node) => ({ value: node, label: node })),
              ]}
            >
              <SelectTrigger
                size="sm"
                aria-label={t('app.fractal.shared.server')}
                class="w-fit"
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent aria-label={t('app.fractal.shared.server')}>
                <SelectItem value={EVERY}>
                  {t('app.fractal.shared.top')}
                </SelectItem>
                {#each nodes as node (node)}
                  <SelectItem value={node}>{node}</SelectItem>
                {/each}
              </SelectContent>
            </Select>
          </div>
        {/if}
      </div>

      <FaultAlert {fault} />

      {#if stores.loading}
        <ListingSkeleton label={t('app.fractal.shared.loading')} art rows={3} />
      {:else if rows.length === 0}
        <EmptyState
          icon={FolderIcon}
          heading={atTop
            ? t('app.fractal.shared.topEmpty.heading')
            : t('app.fractal.shared.folderEmpty')}
          headingLevel="h3"
        />
      {:else}
        <Table>
          <TableCaption class="sr-only">
            {t('app.fractal.shared.caption')}
          </TableCaption>
          <TableHeader>
            <TableRow>
              <TableHead>{t('app.fractal.shared.column.name')}</TableHead>
              <TableHead>{t('app.fractal.shared.column.size')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {#each rows as row (`${row.store.node}|${row.store.instance}|${row.path}`)}
              {@const since = row.store.staleSince}
              <TableRow
                data-state={since === undefined ? undefined : 'stale'}
                class={since === undefined ? undefined : 'opacity-60'}
              >
                <TableCell class="max-w-96">
                  <div class="flex items-center gap-3">
                    <FileThumbnail name={row.name} glyph={glyphOf(row)} />
                    <div class="flex min-w-0 flex-col gap-0.5">
                      {#if row.folder}
                        <Button
                          variant="link"
                          class="min-w-0 text-start"
                          onclick={() => {
                            walk(placeOfFolder(row));
                          }}
                        >
                          <ClippedName name={row.name} />
                        </Button>
                      {:else}
                        <ClippedName name={row.name} class="min-w-0" />
                      {/if}
                      <span
                        class="truncate font-mono text-xs text-muted-foreground"
                      >
                        <span class="font-bold text-foreground"
                          >{row.store.node}</span
                        >
                        · {row.store.instance} · {folderOf(row)}
                      </span>
                      {#if since !== undefined}
                        <span class="text-xs text-muted-foreground">
                          {t('app.fractal.shared.stale', {
                            node: row.store.node,
                            time: formatDateTime(since),
                          })}
                        </span>
                      {/if}
                    </div>
                  </div>
                </TableCell>
                <TableCell class="font-mono text-xs whitespace-nowrap">
                  {row.folder ? '' : formatBytes(row.size)}
                </TableCell>
              </TableRow>
            {/each}
          </TableBody>
        </Table>
      {/if}

      <!-- A store with no top listing is not an empty one, so each says why. -->
      {#each unscanned as store (placeOf(store))}
        <p class="text-xs text-muted-foreground">
          {t('app.fractal.shared.unscanned', {
            node: store.node,
            storage: store.instance,
          })}
        </p>
      {/each}
      {#each stores.unread as store (placeOf(store))}
        <p class="text-xs text-muted-foreground">
          {t('app.fractal.shared.unread', {
            node: store.node,
            storage: store.instance,
          })}
        </p>
      {/each}
    {/if}
  </CardContent>
</Card>
