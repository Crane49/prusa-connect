<!--
  What a tile of one printer draws before the printer itself: a request to
  pick one, the fleet on its way, and a printer this person can no longer
  see. A printer that was removed and a printer that was never chosen ask
  different things of the person, so each has its own sentence
  (ui-integration.md). Once there is a printer, the tile draws it through
  `children`.
-->
<script lang="ts">
  import SearchXIcon from '@lucide/svelte/icons/search-x';
  import type { LucideIcon } from '@lucide/svelte';
  import type { Snippet } from 'svelte';

  import type { PrinterView } from '../printers';
  import { EmptyState } from '../../../primitives';
  import { t } from '../../../i18n';
  import { dashboardContext } from '../context';
  import FleetState from './fleet-state.svelte';

  let {
    device,
    icon,
    pickNote,
    children,
  }: {
    /** The printer the settings name, or empty for none yet. */
    device: string;
    /** The glyph of the tile's kind, drawn while no printer is picked. */
    icon: LucideIcon;
    /** What picking a printer does for this kind of tile. */
    pickNote: string;
    children: Snippet<[PrinterView]>;
  } = $props();

  const { printers } = dashboardContext();

  const view = $derived(device === '' ? undefined : printers.view(device));
</script>

{#if device === ''}
  <EmptyState
    size="sm"
    {icon}
    heading={t('dashboard.printer.pick')}
    description={pickNote}
  />
{:else}
  <FleetState>
    {#if view === undefined}
      <EmptyState
        size="sm"
        icon={SearchXIcon}
        heading={t('dashboard.printer.gone')}
        description={t('dashboard.printer.goneNote')}
      />
    {:else}
      {@render children(view)}
    {/if}
  </FleetState>
{/if}
