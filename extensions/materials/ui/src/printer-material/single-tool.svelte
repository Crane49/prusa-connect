<!--
  A printer with one tool and no MMU, on the Tools tab: the Extruder card with
  the material's circle, its name, brand and badges, and the spool with what
  is left of it; beside it the Tool settings the printer reports; under both
  the Material details of the assigned material.

  The settings are read-only here: changing a nozzle is the printer's own
  business, not the material's.
-->
<script lang="ts">
  import { formatNumber, formatUnit } from '@prusa/connect-ui/i18n';
  import { Badge, Card, CardContent } from '@prusa/connect-ui/primitives';
  import type { Snippet } from 'svelte';

  import type { Detail } from '../api';
  import { heldHost } from '../host';
  import MaterialDetails from '../material-page/material-details.svelte';
  import MaterialCircle from './material-circle.svelte';
  import SpoolBar from './spool-bar.svelte';
  import ToolIcon from './tool-icon.svelte';
  import { firstText, shareLeft, type ToolView } from './tool-view';

  let {
    view,
    detail,
    actions,
  }: {
    view: ToolView;
    /** The assigned material's details, once read. */
    detail: Detail | undefined;
    actions: Snippet;
  } = $props();

  const t = heldHost().t;

  const nozzle = $derived(view.tool.nozzle);
  const share = $derived(shareLeft(view));
  const type = $derived(
    firstText(view.tool.assignment?.type, view.tool.reported?.type),
  );

  const grams = (g: number): string =>
    formatNumber(g, { maximumFractionDigits: 0 });
  const mm = (d: number): string => formatUnit(d, 'mm');
  const onOff = (value: boolean | undefined): string => {
    if (value === undefined) return '—';
    return value ? t('materials.tools.on') : t('materials.tools.off');
  };

  /** The Tool settings rows. High temperature only when the printer says. */
  const settings = $derived([
    {
      label: t('materials.tools.nozzleDiameter'),
      value: nozzle.diameter_mm === undefined ? '—' : mm(nozzle.diameter_mm),
    },
    {
      label: t('materials.tools.toolType'),
      value: t('materials.tools.printHead'),
    },
    { label: t('materials.tools.highFlow'), value: onOff(nozzle.high_flow) },
    { label: t('materials.tools.hardened'), value: onOff(nozzle.hardened) },
    ...(nozzle.high_temp === undefined
      ? []
      : [
          {
            label: t('materials.tools.highTemp'),
            value: onOff(nozzle.high_temp),
          },
        ]),
  ]);
</script>

<div data-part="single-tool" class="@container flex flex-col gap-4">
  <div class="cards grid gap-4">
    <Card>
      <CardContent class="flex flex-col gap-4">
        <div class="flex items-center justify-between gap-2">
          <h3 class="font-semibold">{view.label}</h3>
          <span class="flex items-center gap-2 text-muted-foreground">
            {#if nozzle.high_flow === true}
              <Badge variant="secondary" title={t('materials.tools.highFlow')}>
                {t('materials.tools.hf')}
              </Badge>
            {/if}
            {#if nozzle.high_temp === true}
              <Badge variant="secondary" title={t('materials.tools.highTemp')}>
                {t('materials.tools.ht')}
              </Badge>
            {/if}
            {#if nozzle.diameter_mm !== undefined}
              {mm(nozzle.diameter_mm)}
              <ToolIcon name="nozzle" class="size-4 text-primary" />
            {/if}
          </span>
        </div>

        <div class="flex items-center gap-6">
          <MaterialCircle
            text={view.circle}
            colorRgba={view.colorRgba}
            size="large"
            ring={view.ring}
            title={t('materials.tools.loaded')}
          />
          <div class="flex min-w-0 flex-col gap-2">
            {#if view.name === undefined}
              <span class="text-muted-foreground">
                {t('materials.printer.none')}
              </span>
            {:else}
              <span class="text-xl font-semibold">{view.name}</span>
            {/if}
            {#if view.brand !== ''}
              <span class="text-muted-foreground">{view.brand}</span>
            {/if}
            <span class="flex flex-wrap gap-2">
              {#if type !== ''}
                <Badge variant="secondary">{type}</Badge>
              {/if}
              {#if view.fromMaterials}
                <Badge variant="secondary">
                  <ToolIcon name="spool" class="size-3" />
                  {t('materials.printer.fromMaterials')}
                </Badge>
              {/if}
              {#if view.tool.from_nfc}
                <Badge
                  variant="secondary"
                  title={t('materials.printer.nfcTitle')}
                >
                  <ToolIcon name="nfc" class="size-3" />
                  {t('materials.printer.nfcBadge')}
                </Badge>
              {/if}
            </span>
          </div>
        </div>

        {#if view.leftG !== undefined}
          <div data-part="spool" class="flex flex-col gap-2">
            <div class="flex justify-between gap-2 font-semibold">
              <span>{t('materials.tools.spool')}</span>
              <span>
                {view.fullG === undefined
                  ? t('materials.printer.grams', { g: grams(view.leftG) })
                  : t('materials.printer.weight', {
                      left: grams(view.leftG),
                      full: grams(view.fullG),
                    })}
              </span>
            </div>
            <SpoolBar left={share} colorRgba={view.colorRgba} thick />
            <span class="text-sm text-muted-foreground">
              {share === undefined
                ? t('materials.tools.left', { g: grams(view.leftG) })
                : t('materials.tools.leftShare', {
                    g: grams(view.leftG),
                    pct: Math.round(share * 100),
                  })}
            </span>
          </div>
        {/if}

        {@render actions()}
      </CardContent>
    </Card>

    <Card>
      <CardContent class="flex flex-col gap-2">
        <h3 class="font-semibold">{t('materials.tools.settings')}</h3>
        <dl data-part="tool-settings" class="flex flex-col text-sm">
          {#each settings as row (row.label)}
            <div
              class="flex justify-between gap-4 border-b border-border py-2 last:border-b-0"
            >
              <dt class="text-muted-foreground">{row.label}</dt>
              <dd class="font-semibold">{row.value}</dd>
            </div>
          {/each}
        </dl>
      </CardContent>
    </Card>
  </div>

  {#if detail !== undefined}
    <Card data-part="material-details">
      <CardContent class="flex flex-col gap-4">
        <h3 class="font-semibold">{t('materials.tools.details')}</h3>
        <MaterialDetails {detail} bare />
      </CardContent>
    </Card>
  {/if}
</div>

<style>
  /* Side by side, the tool wider, once there is room; one column below. */
  @container (min-width: 48rem) {
    .cards {
      grid-template-columns: 3fr 2fr;
    }
  }
</style>
