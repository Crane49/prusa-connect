<!--
  What a tile that reads the fleet draws before the fleet can be read: a
  skeleton while it is on its way, and a sentence when it could not be read.
  Once it is read the tile draws itself, through `children`.

  An empty fleet and a fleet not read yet are different facts, so the two
  never share a drawing (ui-integration.md).
-->
<script lang="ts">
  import { EmptyState, Skeleton } from '../../../primitives';
  import CloudOffIcon from '@lucide/svelte/icons/cloud-off';
  import type { Snippet } from 'svelte';

  import { t } from '../../../i18n';
  import { dashboardContext } from '../context';

  let { children }: { children: Snippet } = $props();

  const { printers } = dashboardContext();
</script>

{#if printers.loaded}
  {@render children()}
{:else if printers.error !== undefined}
  <EmptyState
    size="sm"
    icon={CloudOffIcon}
    heading={t('dashboard.fleet.unreadable')}
    description={t('dashboard.fleet.unreadableNote')}
  />
{:else}
  <div
    role="status"
    aria-label={t('dashboard.fleet.loading')}
    class="flex h-full flex-col gap-2"
  >
    <Skeleton class="h-5 w-2/3" />
    <Skeleton class="h-2 w-full" />
    <Skeleton class="w-full flex-1" />
  </div>
{/if}
