<!--
  The spools of one material: the id, with an NFC badge on a spool a printer
  read, the package it came as with its empty weight, the weight left as a bar
  against the full weight, the printer tool it is on, where it
  is kept and the notes. The actions are drawn only when the person may write,
  and so is the box that leads each row: ticked rows are deleted together,
  shift-click ticks a range, and the box in the header ticks or clears every
  row on no tool.

  An archived spool is marked, with its reason on hover, and has neither
  Assign nor Merge, since it cannot go on a tool.

  A spool on a printer tool cannot be deleted until it is unassigned, so its
  Delete and box are disabled, and a tooltip on Delete says why. Its Edit
  stays, for its notes and where it is kept. A spool on a tool that is gone,
  of a printer that left or a tool the printer lost, has Unassign, since no
  printer page shows that tool. A spool added by hand that is on no tool may
  be merged into a spool a printer read.
-->
<script lang="ts">
  import {
    Badge,
    Button,
    Popover,
    PopoverContent,
    PopoverTrigger,
    Progress,
    SelectAllCheckbox,
    SelectCheckbox,
    type Selection,
    Table,
    TableBody,
    TableCell,
    TableHead,
    TableHeader,
    TableRow,
  } from '@prusa/connect-ui/primitives';

  import type { Detail, Printer, Spool } from '../api';
  import { heldHost } from '../host';
  import { OWN_MODULE } from '../own-module';
  import { toolLabel } from '../printer-material/tool-label';
  import TapTooltip from '../tap-tooltip.svelte';

  let {
    spools,
    packages,
    mayWrite,
    printers,
    mergeable,
    onassign,
    onunassign,
    onmerge,
    onedit,
    oncopy,
    ondelete,
    selection,
  }: {
    spools: readonly Spool[];
    /** The material's packages, which name a spool's package_uuid. */
    packages: Detail['packages'];
    mayWrite: boolean;
    /** The printers the person may observe, which name a spool's tool. */
    printers: readonly Printer[];
    /**
     * Whether the material may hold a printer spool, loaded or on a page not
     * read yet, which a spool added by hand merges into.
     */
    mergeable: boolean;
    onassign: (spool: Spool) => void;
    /** Clears the tool, gone from its printer, that the spool is on. */
    onunassign: (spool: Spool) => void;
    onmerge: (spool: Spool) => void;
    onedit: (spool: Spool) => void;
    oncopy: (spool: Spool) => void;
    ondelete: (spool: Spool) => void;
    /** The ticked spools, by id. */
    selection: Selection<number>;
  } = $props();

  const t = heldHost().t;

  /** The spools a delete can take: those on no printer tool. */
  const free = $derived(spools.filter((s) => s.tool == null).map((s) => s.id));

  /** The printer and tool a spool is on, such as Core One · Tool 1. */
  const placeOf = (tool: NonNullable<Spool['tool']>): string =>
    t('materials.spools.on', {
      printer:
        printers.find((p) => p.id === tool.device_id)?.name ??
        t('materials.spools.unknownPrinter'),
      tool: toolLabel(t, tool.tool_id),
    });

  // The package by its name, and the empty weight the spool itself keeps,
  // which is the package's unless the person typed another.
  const container = (spool: Spool): string => {
    const known = packages.find((p) => p.uuid === spool.package_uuid);
    const parts = [
      known === undefined ? '' : known.container_name || known.slug,
      spool.empty_weight_g === undefined
        ? ''
        : t('materials.spool.packageEmpty', { g: spool.empty_weight_g }),
    ].filter((part) => part !== '');

    return parts.length === 0 ? '—' : parts.join(' · ');
  };
</script>

<Table>
  <TableHeader>
    <TableRow>
      {#if mayWrite}
        <TableHead class="w-8">
          <SelectAllCheckbox
            {selection}
            keys={free}
            disabled={free.length === 0}
            label={t('materials.spools.selectAll')}
          />
        </TableHead>
      {/if}
      <TableHead>{t('materials.spools.id')}</TableHead>
      <TableHead>{t('materials.spools.container')}</TableHead>
      <TableHead>{t('materials.column.weight')}</TableHead>
      <TableHead>{t('materials.spools.printer')}</TableHead>
      <TableHead>{t('materials.spool.location')}</TableHead>
      <TableHead>{t('materials.spool.notes')}</TableHead>
      <TableHead></TableHead>
    </TableRow>
  </TableHeader>
  <TableBody>
    {#each spools as spool (spool.id)}
      <!-- The bar measures filament: what is left against what a full spool
           held, which is the full weight less the empty spool's. -->
      {@const full = Math.max(
        (spool.full_weight_g ?? spool.remaining_weight_g) -
          (spool.full_weight_g === undefined ? 0 : (spool.empty_weight_g ?? 0)),
        spool.remaining_weight_g,
        1,
      )}
      {@const assigned = spool.tool != null}
      {@const archived = spool.archived_at != null}
      <TableRow
        data-part="spool"
        data-spool={spool.id}
        data-state={selection.has(spool.id) ? 'selected' : undefined}
      >
        {#if mayWrite}
          <TableCell>
            <SelectCheckbox
              {selection}
              key={spool.id}
              disabled={assigned}
              label={t('materials.spools.select', { id: spool.id })}
            />
          </TableCell>
        {/if}
        <TableCell>
          <span class="flex items-center gap-2">
            {spool.id}
            {#if spool.source === 'printer'}
              <Badge
                variant="secondary"
                title={t('materials.printer.nfcTitle')}
              >
                {t('materials.printer.nfcBadge')}
              </Badge>
            {/if}
            {#if archived}
              <Badge
                variant="outline"
                title={spool.archive_reason === undefined
                  ? undefined
                  : t(`materials.archive.reason.${spool.archive_reason}`)}
              >
                {t('materials.archive.badge')}
              </Badge>
            {/if}
          </span>
        </TableCell>
        <TableCell>{container(spool)}</TableCell>
        <TableCell>
          <div class="flex min-w-32 flex-col gap-1">
            <span class="text-sm whitespace-nowrap">
              {t('materials.spool.grams', { g: spool.remaining_weight_g })}
            </span>
            <Progress
              value={spool.remaining_weight_g}
              max={full}
              aria-label={t('materials.spools.bar', { id: spool.id })}
            />
          </div>
        </TableCell>
        <TableCell>
          {#if spool.tool != null}
            <span class="whitespace-nowrap">{placeOf(spool.tool)}</span>
          {:else}
            —
          {/if}
        </TableCell>
        <!-- Typed text of any length is cut to one line so it cannot push the
             table past the card: the location shows the rest on hover, a
             note opens in a popover. -->
        <TableCell>
          <span class="block max-w-40 truncate" title={spool.location}>
            {spool.location}
          </span>
        </TableCell>
        <TableCell>
          {#if spool.notes !== ''}
            <Popover>
              <PopoverTrigger>
                {#snippet child({ props })}
                  <Button
                    {...props}
                    variant="link"
                    class="block max-w-64 truncate text-left"
                    aria-label={t('materials.spools.notesOf', { id: spool.id })}
                  >
                    {spool.notes}
                  </Button>
                {/snippet}
              </PopoverTrigger>
              <!-- Portalled, so it carries the module's name for its
                   utilities; the line breaks the person typed are kept. -->
              <PopoverContent align="start" data-module={OWN_MODULE}>
                <p class="text-sm break-words whitespace-pre-wrap">
                  {spool.notes}
                </p>
              </PopoverContent>
            </Popover>
          {/if}
        </TableCell>
        <TableCell>
          {#if mayWrite}
            <div class="flex gap-1">
              {#if !assigned && !archived}
                <Button
                  variant="ghost"
                  size="sm"
                  aria-label={t('materials.spools.assign', { id: spool.id })}
                  onclick={() => {
                    onassign(spool);
                  }}
                >
                  {t('materials.spools.assignLabel')}
                </Button>
              {/if}
              {#if spool.tool?.gone === true}
                <Button
                  variant="ghost"
                  size="sm"
                  aria-label={t('materials.spools.unassign', { id: spool.id })}
                  onclick={() => {
                    onunassign(spool);
                  }}
                >
                  {t('materials.printer.unassign')}
                </Button>
              {/if}
              {#if mergeable && !assigned && !archived && spool.source === 'manual'}
                <Button
                  variant="ghost"
                  size="sm"
                  aria-label={t('materials.spools.merge', { id: spool.id })}
                  onclick={() => {
                    onmerge(spool);
                  }}
                >
                  {t('materials.spools.mergeLabel')}
                </Button>
              {/if}
              <Button
                variant="ghost"
                size="sm"
                aria-label={t('materials.spools.edit', { id: spool.id })}
                onclick={() => {
                  onedit(spool);
                }}
              >
                {t('materials.spools.editLabel')}
              </Button>
              <Button
                variant="ghost"
                size="sm"
                aria-label={t('materials.spools.copy', { id: spool.id })}
                onclick={() => {
                  oncopy(spool);
                }}
              >
                {t('materials.spools.copyLabel')}
              </Button>
              {#snippet remove()}
                <Button
                  variant="ghost"
                  size="sm"
                  disabled={assigned}
                  aria-label={t('materials.spools.delete', { id: spool.id })}
                  onclick={() => {
                    ondelete(spool);
                  }}
                >
                  {t('materials.spools.deleteLabel')}
                </Button>
              {/snippet}
              {#if assigned}
                <TapTooltip
                  text={t('materials.spools.deleteAssigned')}
                  children={remove}
                />
              {:else}
                {@render remove()}
              {/if}
            </div>
          {/if}
        </TableCell>
      </TableRow>
    {/each}
  </TableBody>
</Table>
