<!--
  One tool, or one MMU slot, as a tile of the Tools tab: the label and the
  nozzle, a thin bar of what is left on the spool, where the spool came from,
  the material's circle, its name and brand, the grams, then the buttons. The
  rows keep their height when empty, so tiles side by side line up. The tile
  whose picker is open is outlined.
-->
<script lang="ts">
  import { formatNumber } from '@prusa/connect-ui/i18n';
  import type { Snippet } from 'svelte';

  import { heldHost } from '../host';
  import MaterialCircle from './material-circle.svelte';
  import SpoolBar from './spool-bar.svelte';
  import ToolIcon from './tool-icon.svelte';
  import { shareLeft, type ToolView } from './tool-view';

  let {
    view,
    open,
    actions,
  }: {
    view: ToolView;
    /** The tile's picker is open. */
    open: boolean;
    actions: Snippet;
  } = $props();

  const t = heldHost().t;

  const grams = (g: number): string =>
    formatNumber(g, { maximumFractionDigits: 0 });
</script>

<li
  data-part="tool"
  data-tool={view.tool.id}
  class={[
    'flex flex-col items-center gap-1.5 rounded-md border p-2 text-center',
    open ? 'border-primary' : 'border-border hover:border-foreground',
  ]}
>
  <div
    class="flex w-full items-center justify-between gap-2 text-xs text-muted-foreground"
  >
    <span class="truncate">{view.label}</span>
    {#if view.tool.nozzle.diameter_mm !== undefined}
      <span class="flex items-center gap-1">
        {formatNumber(view.tool.nozzle.diameter_mm)}
        <ToolIcon
          name="nozzle"
          class="size-3.5 text-primary"
          title={t('materials.tools.nozzle')}
        />
      </span>
    {/if}
  </div>
  <SpoolBar left={shareLeft(view)} colorRgba={view.colorRgba} />
  <div class="flex h-4 items-center gap-1 text-muted-foreground">
    {#if view.fromMaterials}
      <ToolIcon
        name="spool"
        class="size-3.5"
        title={t('materials.printer.fromMaterials')}
      />
    {/if}
    {#if view.tool.from_nfc}
      <ToolIcon
        name="nfc"
        class="size-3.5"
        title={t('materials.printer.nfcTitle')}
      />
    {/if}
  </div>
  <MaterialCircle
    text={view.circle}
    colorRgba={view.colorRgba}
    size="small"
    ring={view.ring}
    title={view.tool.active
      ? t('materials.tools.inHead')
      : t('materials.tools.loaded')}
  />
  <span
    class={[
      'line-clamp-2 min-h-8 text-xs font-medium',
      view.name === undefined && 'text-muted-foreground',
    ]}
  >
    {view.name ?? t('materials.printer.none')}
  </span>
  <span class="h-4 w-full truncate text-xs text-muted-foreground">
    {view.brand}
  </span>
  <span class="h-4 text-xs">
    {#if view.leftG !== undefined}
      {view.fullG === undefined
        ? t('materials.printer.grams', { g: grams(view.leftG) })
        : t('materials.printer.weight', {
            left: grams(view.leftG),
            full: grams(view.fullG),
          })}
    {/if}
  </span>
  <div class="mt-auto w-full">
    {@render actions()}
  </div>
</li>
