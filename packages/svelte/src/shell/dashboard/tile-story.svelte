<!--
  One core tile on its own, for the dashboard's stories. It sets the context
  the screen sets and holds the settings to the tile's schema the way the
  grid does, so a story names only the settings it changes.
-->
<script lang="ts">
  import type { Component } from 'svelte';

  import { TooltipProvider } from '../../primitives';
  import {
    type CardProps,
    type CardSettings,
    readCardSettings,
  } from '../../slots';
  import { CORE_CARDS } from './cards/declarations';
  import { type DashboardContext, setDashboardContext } from './context';

  let {
    context,
    tile: Tile,
    type,
    config = {},
    title = '',
  }: {
    context: DashboardContext;
    tile: Component<CardProps>;
    /** The core card's id, whose schema the settings are held to. */
    type: string;
    config?: CardSettings;
    title?: string;
  } = $props();

  // svelte-ignore state_referenced_locally
  setDashboardContext(context);

  const declaration = $derived(CORE_CARDS.find((card) => card.id === type));
  const settings = $derived(
    declaration === undefined
      ? {}
      : readCardSettings(declaration.settings, config),
  );
  const size = $derived(declaration?.size ?? { w: 4, h: 4 });
</script>

<TooltipProvider>
  <div
    class="flex h-80 w-96 flex-col overflow-hidden rounded-sm border border-border bg-card p-3 text-card-foreground"
  >
    <Tile config={settings} {title} {size} editing={false} />
  </div>
</TooltipProvider>
