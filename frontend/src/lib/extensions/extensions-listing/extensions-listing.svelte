<!--
  Every module of the Extensions room, in whichever of the two shapes fits:
  a table where there is room for one, stacked rows where there is not or
  where the reader chose them.

  A row names the module by its title. Its path, such as github.com/acme/spools,
  names it for the machine and is no link to a repository, so the listing leaves
  it to the module's own page.

  A row says what the module is, how far the marketplace vouches for it, who
  publishes it, which release is on offer and which one runs here, where it
  comes from and what state it is in. The act its state calls for sits at the
  end of the row. The trust badge sits beside the name, so it is read at every
  width, in the table and in the stacked rows alike.

  The words of a row's switch, such as what the module does now and why the
  switch is locked, are drawn in the row's body and not beside the switch.
  There they wrap, so a long reason never pushes the row wider than the page.
  When the table is narrow the columns give way from the least read: memory,
  then the source, then the author, then the status, which the act at the end
  of the row already says. The name, the release and the act always stay.

  A row names its module in `data-extension` and never in `data-module`. An
  installed module's styles apply under any `[data-module]`, so that name on
  a row would let them restyle the host's own row.
-->
<script lang="ts">
  import BadgeCheckIcon from '@lucide/svelte/icons/badge-check';
  import ExternalLinkIcon from '@lucide/svelte/icons/external-link';
  import PackageIcon from '@lucide/svelte/icons/package';
  import ServerIcon from '@lucide/svelte/icons/server';
  import ShieldAlertIcon from '@lucide/svelte/icons/shield-alert';
  import ShieldCheckIcon from '@lucide/svelte/icons/shield-check';
  import StoreIcon from '@lucide/svelte/icons/store';
  import { translateFor } from '@prusa/connect-ui/i18n';
  import {
    Badge,
    DataRow,
    type DataRowValue,
    RowList,
    Table,
    TableBody,
    TableCaption,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
  } from '@prusa/connect-ui/primitives';

  import { APP_NAMESPACE } from '../../i18n';
  import type { ModuleCatalogue } from '../../stores/module-catalogue.svelte';
  import { ExtensionActions } from '../extension-actions';
  import { ModuleMemory } from '../module-memory';
  import { failuresText, usageText } from '../memory';
  import type { AskingAct } from '../extension-dialog';
  import { extensionDetailHref } from '../../routes';
  import { type ExtensionRow, SOURCE_KEY } from '../rows';
  import { observedOf, switchNotesOf, switchOf, whyOf } from '../switch';
  import type { ExtensionFieldId } from '../types';

  let {
    rows,
    view,
    catalogue,
    shows,
    go,
    ask,
    highlighted,
  }: {
    rows: readonly ExtensionRow[];
    /** `table` or `rows`, as the toolbar's view switch holds it. */
    view: string;
    catalogue: ModuleCatalogue;
    shows: (field: ExtensionFieldId) => boolean;
    go: (href: string) => void;
    ask: (row: ExtensionRow, act: AskingAct, version?: string) => void;
    /** The key of the row a link pointed at, drawn so the eye finds it. */
    highlighted?: string | undefined;
  } = $props();

  const t = translateFor(APP_NAMESPACE);

  // Every key is written out rather than assembled from a value.
  const STATE_KEY = {
    available: 'app.settings.extensions.offer.state.available',
    installed: 'app.settings.extensions.offer.state.installed',
    update: 'app.settings.extensions.offer.state.update',
  } as const;

  const STATE_VARIANT = {
    available: 'outline',
    installed: 'success',
    update: 'warning',
  } as const;

  const TRUST_KEY: Readonly<Record<string, string>> = {
    official: 'app.settings.extensions.offer.trust.official',
    verified: 'app.settings.extensions.offer.trust.verified',
    community: 'app.settings.extensions.offer.trust.community',
  };

  // The colour says how far the page vouches for a module: green for Prusa
  // Research's own, blue for a reviewed one, red for one nobody reviewed. The
  // brand fill stays with Install, the one thing to press on a row. Each
  // level has its own icon too, so the colour is never the only sign.
  const TRUST_VARIANT: Readonly<
    Record<string, 'success' | 'info' | 'destructive'>
  > = {
    official: 'success',
    verified: 'info',
    community: 'destructive',
  };

  // Only an installed module has a detail page to open.
  const detailHrefOf = (row: ExtensionRow): string | undefined =>
    row.instance !== undefined && row.namespace
      ? extensionDetailHref(row.namespace)
      : undefined;

  const trustOf = (row: ExtensionRow): string | undefined => {
    const key = row.trust === undefined ? undefined : TRUST_KEY[row.trust];

    return key === undefined ? undefined : t(key);
  };

  // The installed release is named only when it is not the one on offer:
  // the same number twice says nothing.
  const installedOther = (row: ExtensionRow): string | undefined =>
    row.installedVersion !== undefined &&
    row.installedVersion !== '' &&
    row.installedVersion !== row.version
      ? row.installedVersion
      : undefined;

  // Why an installed module is not running, such as a record backend it
  // needs and the server does not run.
  const whyNotRunning = (row: ExtensionRow) =>
    row.instance === undefined ? undefined : whyOf(row.instance, t);

  // What the switch at the end of the row would say beside itself.
  const notesOf = (row: ExtensionRow) =>
    switchNotesOf(
      row,
      switchOf(
        row,
        catalogue.mayConfigure(row.kind),
        catalogue.modulesOn,
        catalogue.safeMode,
      ),
      t,
    );

  // Only a service module runs a process of its own, so only it has a budget.
  const memoryOf = (row: ExtensionRow) =>
    row.instance?.runtime === 'service'
      ? row.instance.status.memory
      : undefined;

  // A module that is not running has no use to read.
  const runsOf = (row: ExtensionRow): boolean =>
    row.instance !== undefined &&
    ['running', 'degraded', 'starting'].includes(observedOf(row.instance));

  const secondaries = (row: ExtensionRow): DataRowValue[] => {
    const notes = notesOf(row);
    const memory = shows('memory') ? memoryOf(row) : undefined;

    return [
      row.channel === 'beta' ? t('app.settings.extensions.beta') : undefined,
      shows('author') && row.author !== '' ? row.author : undefined,
      shows('version') && row.version !== '' ? row.version : undefined,
      shows('source') ? t(SOURCE_KEY[row.source]) : undefined,
      shows('state') ? t(STATE_KEY[row.state]) : undefined,
      memory === undefined ? undefined : usageText(memory, t, runsOf(row)),
      memory === undefined ? undefined : failuresText(memory, t),
      notes.observed,
      notes.locked,
      whyNotRunning(row)?.title,
    ];
  };
</script>

{#snippet sourceBadge(row: ExtensionRow)}
  <Badge variant="outline">
    {#if row.source === 'marketplace'}
      <StoreIcon aria-hidden="true" />
    {:else if row.source === 'local'}
      <ServerIcon aria-hidden="true" />
    {:else}
      <PackageIcon aria-hidden="true" />
    {/if}
    {t(SOURCE_KEY[row.source])}
  </Badge>
{/snippet}

{#snippet betaBadge(row: ExtensionRow)}
  {#if row.channel === 'beta'}
    <Badge variant="warning" data-part="beta">
      {t('app.settings.extensions.beta')}
    </Badge>
  {/if}
{/snippet}

{#snippet stateBadge(row: ExtensionRow)}
  <Badge variant={STATE_VARIANT[row.state]} data-part="state">
    {t(STATE_KEY[row.state])}
  </Badge>
{/snippet}

{#snippet trustBadge(row: ExtensionRow)}
  {@const trust = trustOf(row)}
  {#if trust !== undefined}
    <Badge
      variant={TRUST_VARIANT[row.trust ?? ''] ?? 'info'}
      data-part="trust"
      data-trust={row.trust}
    >
      {#if row.trust === 'official'}
        <BadgeCheckIcon aria-hidden="true" />
      {:else if row.trust === 'verified'}
        <ShieldCheckIcon aria-hidden="true" />
      {:else if row.trust === 'community'}
        <ShieldAlertIcon aria-hidden="true" />
      {/if}
      {trust}
    </Badge>
  {/if}
{/snippet}

<!-- The marketplace is another site, so its page opens in a window of its own. -->
{#snippet marketplaceLink(row: ExtensionRow)}
  {#if row.pageUrl}
    <a
      href={row.pageUrl}
      target="_blank"
      rel="noopener noreferrer"
      class="inline-flex rounded-sm text-muted-foreground hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background focus-visible:outline-none"
      aria-label={t('app.settings.extensions.offer.marketplaceLink.label', {
        name: row.title,
      })}
      title={t('app.settings.extensions.offer.open')}
      data-part="marketplace-link"
    >
      <ExternalLinkIcon aria-hidden="true" class="size-3.5" />
    </a>
  {/if}
{/snippet}

{#snippet table()}
  <Table>
    <TableCaption class="sr-only">
      {catalogue.modulesOn
        ? t('app.settings.extensions.list.caption')
        : t('app.settings.extensions.list.captionModulesOff')}
    </TableCaption>
    <TableHeader>
      <TableRow>
        <TableHead>{t('app.settings.extensions.column.module')}</TableHead>
        {#if shows('author')}
          <TableHead class="hidden @5xl/extensions:table-cell">
            {t('app.settings.extensions.column.author')}
          </TableHead>
        {/if}
        {#if shows('version')}
          <TableHead>{t('app.settings.extensions.column.version')}</TableHead>
        {/if}
        {#if shows('source')}
          <TableHead class="hidden @6xl/extensions:table-cell">
            {t('app.settings.extensions.column.source')}
          </TableHead>
        {/if}
        {#if shows('state')}
          <TableHead class="hidden @4xl/extensions:table-cell">
            {t('app.settings.extensions.column.state')}
          </TableHead>
        {/if}
        {#if shows('memory')}
          <TableHead class="hidden @7xl/extensions:table-cell">
            {t('app.settings.extensions.column.memory')}
          </TableHead>
        {/if}
        <TableHead class="sr-only">
          {t('app.settings.extensions.column.actions')}
        </TableHead>
      </TableRow>
    </TableHeader>
    <TableBody>
      {#each rows as row (row.key)}
        {@const other = installedOther(row)}
        {@const why = whyNotRunning(row)}
        {@const notes = notesOf(row)}
        <TableRow
          data-extension={row.module}
          data-state={row.state}
          data-highlighted={highlighted === row.key || undefined}
          class="data-highlighted:bg-accent"
          aria-busy={catalogue.acting === row.module}
        >
          <TableCell class="max-w-md">
            <!-- A table cell keeps to one line, and white-space is inherited,
                 so the content says it may wrap: the summary takes two. -->
            <span class="flex min-w-0 flex-col gap-0.5 whitespace-normal">
              <span class="flex flex-wrap items-center gap-2">
                {#if detailHrefOf(row) !== undefined}
                  <a
                    href={detailHrefOf(row)}
                    class="rounded-sm font-medium underline-offset-4 hover:underline focus-visible:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background focus-visible:outline-none"
                    data-part="detail-link"
                  >
                    {row.title}
                  </a>
                {:else}
                  <span class="font-medium">{row.title}</span>
                {/if}
                {@render trustBadge(row)}
                {@render betaBadge(row)}
                {@render marketplaceLink(row)}
              </span>
              {#if row.summary !== ''}
                <span class="line-clamp-2 text-xs text-muted-foreground">
                  {row.summary}
                </span>
              {/if}
              {#if notes.observed !== undefined || notes.locked !== undefined}
                <span
                  class="flex flex-wrap gap-x-2 text-xs text-muted-foreground"
                  data-part="switch-notes"
                >
                  {#if notes.observed !== undefined}
                    <span data-part="observed">{notes.observed}</span>
                  {/if}
                  {#if notes.locked !== undefined}
                    <span data-part="locked">{notes.locked}</span>
                  {/if}
                </span>
              {/if}
              {#if why !== undefined}
                <span
                  class="text-xs text-destructive"
                  data-part="why"
                  title={why.detail}
                >
                  {why.title}
                  {#if why.action !== undefined}
                    <span class="block text-muted-foreground">{why.action}</span
                    >
                  {/if}
                </span>
              {/if}
            </span>
          </TableCell>
          {#if shows('author')}
            <TableCell class="hidden @5xl/extensions:table-cell">
              {#if row.author !== ''}<span>{row.author}</span>{/if}
            </TableCell>
          {/if}
          {#if shows('version')}
            <TableCell class="tabular-nums">
              <span class="flex flex-col">
                <span class="font-mono text-sm" data-part="version">
                  {row.version}
                </span>
                {#if other !== undefined}
                  <span
                    class="font-mono text-xs text-muted-foreground"
                    data-part="installed-version"
                  >
                    {t('app.settings.extensions.version.installed', {
                      version: other,
                    })}
                  </span>
                {/if}
              </span>
            </TableCell>
          {/if}
          {#if shows('source')}
            <TableCell class="hidden @6xl/extensions:table-cell">
              {@render sourceBadge(row)}
            </TableCell>
          {/if}
          {#if shows('state')}
            <TableCell class="hidden @4xl/extensions:table-cell">
              {@render stateBadge(row)}
            </TableCell>
          {/if}
          {#if shows('memory')}
            <TableCell class="hidden @7xl/extensions:table-cell">
              <ModuleMemory memory={memoryOf(row)} running={runsOf(row)} />
            </TableCell>
          {/if}
          <TableCell class="text-right">
            <ExtensionActions {row} {catalogue} {go} {ask} notes={false} />
          </TableCell>
        </TableRow>
      {/each}
    </TableBody>
  </Table>
{/snippet}

{#snippet stacked()}
  <RowList>
    {#each rows as row (row.key)}
      {#snippet rowTrust()}
        <span class="relative z-10 inline-flex items-center gap-2">
          {@render trustBadge(row)}
          {@render marketplaceLink(row)}
        </span>
      {/snippet}
      <DataRow
        primary={row.title}
        href={detailHrefOf(row)}
        wrapValues
        secondaryValues={[
          (trustOf(row) !== undefined || Boolean(row.pageUrl)) && rowTrust,
          ...secondaries(row),
        ]}
        data-extension={row.module}
        data-state={row.state}
        data-highlighted={highlighted === row.key || undefined}
        class="data-highlighted:bg-accent"
        aria-busy={catalogue.acting === row.module}
      >
        {#snippet actions()}
          <ExtensionActions {row} {catalogue} {go} {ask} notes={false} />
        {/snippet}
      </DataRow>
    {/each}
  </RowList>
{/snippet}

<div class="@container/extensions">
  {#if view === 'table'}
    <div class="hidden @3xl/extensions:block">{@render table()}</div>
    <div class="@3xl/extensions:hidden">{@render stacked()}</div>
  {:else}
    {@render stacked()}
  {/if}
</div>
