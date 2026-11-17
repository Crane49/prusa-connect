<!--
  One printer, as a tile draws it: the drawing or a still frame, its name and
  state, the print with its bar and the time left, the heaters and the
  material.

  It reads like the core's own printer card, from the same primitives, so a
  printer on a dashboard is the printer a person knows from the Printers
  screen. Each fact is drawn only when the tile's settings ask for it, and a
  fact the printer never reported reads as a dash and never as a zero.

  The whole face links to the printer's own page. The layout follows the size
  of the box it sits in (frontend-css.md, container queries): the drawing
  shows once there is room beside the name, and the heaters fold under each
  other in a narrow tile.
-->
<script lang="ts">
  import { formatDate, formatDuration, formatUnit } from '../../../i18n';
  import {
    PRINTER_STATE_LABEL_KEY,
    type PrinterSeverityTier,
  } from '../../../domain';
  import {
    Badge,
    HeatbedIcon,
    NozzleIcon,
    PrinterIcon,
    SpoolIcon,
    TemperatureReading,
  } from '../../../primitives';

  import type { PrinterView } from '../printers';
  import {
    SEVERITY_LABEL_KEY,
    severityBadgeVariant,
    stateBadgeVariant,
  } from '../../printers/presentation';
  import { t } from '../../../i18n';
  import { dashboardContext } from '../context';
  import type { Picture, PrinterField } from './config';
  import JobBar from './job-bar.svelte';

  let {
    view,
    fields,
    picture = 'art',
    frame,
    dense = false,
  }: {
    view: PrinterView;
    fields: readonly PrinterField[];
    picture?: Picture;
    /** The newest still frame of the printer's camera, when it has one. */
    frame?: string | undefined;
    /** Drawn inside a tile of many printers, where every line counts. */
    dense?: boolean;
  } = $props();

  const { clock } = dashboardContext();

  // One heater box and its reading, as the core's printer card draws them.
  const BOX =
    'flex min-w-0 items-center justify-center rounded-sm border border-border px-1.5 py-1';
  const VALUE = 'flex min-w-0 items-center truncate tabular-nums';

  // The words of an issue take the tint of how loud it is. Only the two
  // loudest tiers have one, which is the rule the core's printer cards follow.
  const ISSUE_TONE: Readonly<Record<PrinterSeverityTier, string>> = {
    critical: 'text-destructive-text',
    attention: 'text-warning-text',
    'not-reporting': 'text-muted-foreground',
    normal: 'text-muted-foreground',
  };

  const shows = (field: PrinterField): boolean => fields.includes(field);

  const offline = $derived(view.state === 'OFFLINE');
  const stateLabel = $derived(t(PRINTER_STATE_LABEL_KEY[view.state]));
  const remainingText = $derived(
    view.remaining === undefined ? '' : formatDuration(view.remaining),
  );
  // When the print ends, as a time of day. The clock moves it, so a wall
  // that stays open for hours keeps a true answer.
  const endsAt = $derived(
    view.remaining === undefined
      ? ''
      : formatDate(clock.minute + view.remaining * 1000, {
          timeStyle: 'short',
        }),
  );
  const showsHeat = $derived(
    shows('nozzle') || shows('bed') || shows('material'),
  );

  // The clock moves the time of day the print ends. One timer serves every
  // face on the page.
  $effect(() => clock.hold());
</script>

{#snippet nozzleIcon()}
  <NozzleIcon aria-hidden="true" class="me-1" />
{/snippet}

{#snippet bedIcon()}
  <HeatbedIcon aria-hidden="true" class="me-1" />
{/snippet}

<a
  href={view.detailHref}
  data-part="printer-face"
  data-state={view.state}
  class={[
    '@container/face flex h-full min-h-0 min-w-0 flex-col gap-2 rounded-sm outline-none focus-visible:ring-2 focus-visible:ring-ring',
    offline && 'text-muted-foreground',
  ]}
>
  {#if picture === 'camera' && frame !== undefined && !dense}
    <img
      src={frame}
      alt={t('dashboard.printer.frame', { name: view.name })}
      class="aspect-video w-full shrink-0 rounded-sm bg-muted object-cover"
      loading="lazy"
    />
  {/if}

  <div class="flex min-w-0 items-start gap-3">
    {#if picture !== 'none' && (picture === 'art' || frame === undefined)}
      <PrinterIcon
        model={view.artwork}
        reported={view.model}
        class={[
          'hidden shrink-0 @3xs/face:block',
          dense ? 'size-10' : 'size-14',
        ]}
      />
    {/if}
    <div class="flex min-w-0 flex-1 flex-col gap-1">
      <div class="flex items-center justify-between gap-2">
        <p class="min-w-0 truncate text-xs text-muted-foreground">
          {view.model === view.name ? '' : view.model}
        </p>
        {#if shows('state')}
          <Badge
            size="md"
            variant={stateBadgeVariant(view.state)}
            class="shrink-0"
          >
            {stateLabel}
          </Badge>
        {/if}
      </div>
      <p
        class={[
          'truncate font-bold',
          dense ? 'text-sm' : 'text-base @xs/face:text-lg',
        ]}
        title={view.name}
      >
        {view.name}
      </p>
    </div>
  </div>

  {#if shows('job') || shows('progress')}
    <div class="flex flex-col gap-1">
      <div
        class="flex items-baseline justify-between gap-3 font-mono text-xs text-muted-foreground"
      >
        {#if shows('job')}
          <span class="min-w-0 truncate" title={view.jobName}>
            {view.jobName ?? t('printers.noJob')}
          </span>
        {:else}
          <span></span>
        {/if}
        {#if shows('progress') && view.progress !== null}
          <span class="shrink-0 text-foreground tabular-nums">
            {formatUnit(view.progress, '%')}
          </span>
        {/if}
      </div>
      {#if shows('progress')}
        <JobBar {view} />
      {/if}
    </div>
  {/if}

  {#if shows('remaining') && view.remaining !== undefined}
    <p
      data-part="printer-remaining"
      class="flex flex-wrap items-baseline gap-x-2 text-sm tabular-nums"
    >
      <span class="font-semibold">
        {t('dashboard.printer.left', { time: remainingText })}
      </span>
      <span class="text-xs text-muted-foreground">
        {t('dashboard.printer.endsAt', { time: endsAt })}
      </span>
    </p>
  {/if}

  {#if showsHeat && !dense}
    <dl
      class="mbs-auto grid grid-cols-1 gap-2 font-mono text-xs @3xs/face:grid-cols-3"
    >
      {#if shows('nozzle')}
        <div class={BOX}>
          <dt class="sr-only">{t('dashboard.printer.nozzle')}</dt>
          <dd class={VALUE}>
            <TemperatureReading
              icon={nozzleIcon}
              current={view.nozzle}
              target={view.nozzleTarget}
              muted={offline}
            />
          </dd>
        </div>
      {/if}
      {#if shows('bed')}
        <div class={BOX}>
          <dt class="sr-only">{t('dashboard.printer.bed')}</dt>
          <dd class={VALUE}>
            <TemperatureReading
              icon={bedIcon}
              current={view.bed}
              target={view.bedTarget}
              muted={offline}
            />
          </dd>
        </div>
      {/if}
      {#if shows('material')}
        <div class={BOX}>
          <dt class="sr-only">{t('dashboard.printer.material')}</dt>
          <dd class={VALUE}>
            <SpoolIcon aria-hidden="true" class="me-1" />
            {#if view.material === undefined}&mdash;{:else}{view.material}{/if}
          </dd>
        </div>
      {/if}
    </dl>
  {/if}

  {#if shows('issue') && view.statusNote !== undefined}
    <p
      data-part="printer-issue"
      class={['flex items-start gap-2 text-sm', ISSUE_TONE[view.tier]]}
    >
      {#if view.severity !== undefined}
        <Badge variant={severityBadgeVariant(view.severity)} class="shrink-0">
          {t(SEVERITY_LABEL_KEY[view.severity])}
        </Badge>
      {/if}
      <span class="line-clamp-2 min-w-0">{view.statusNote}</span>
    </p>
  {/if}
</a>
