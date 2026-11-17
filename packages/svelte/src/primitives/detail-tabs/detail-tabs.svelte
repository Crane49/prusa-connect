<!--
  The strip scrolls and fades its edges (`ScrollStrip`).

  The open tab is the caller's: the strip follows `active` and reports the
  press, so a screen is free to keep the open tab in the path.
-->
<script lang="ts">
  import Badge from '../badge/badge.svelte';
  import Skeleton from '../skeleton/skeleton.svelte';
  import TabsContent from '../tabs/tabs-content.svelte';
  import TabsList from '../tabs/tabs-list.svelte';
  import TabsTrigger from '../tabs/tabs-trigger.svelte';
  import Tabs from '../tabs/tabs.svelte';
  import ScrollStrip from './scroll-strip.svelte';
  import type { DetailTabsProps } from './types';

  let {
    tabs,
    active,
    onChange,
    fallback,
    class: className,
  }: DetailTabsProps = $props();
</script>

<Tabs
  bind:value={() => active, (next: string) => onChange(next)}
  class={['w-full gap-6', className]}
>
  <ScrollStrip {active} {tabs}>
    <TabsList class="h-auto gap-6">
      {#each tabs as tab (tab.id)}
        <TabsTrigger value={tab.id} class="min-h-11">
          {tab.label}
          {#if tab.count !== undefined}
            <Badge variant="default" class="ms-1 tabular-nums"
              >{tab.count}</Badge
            >
          {/if}
        </TabsTrigger>
      {/each}
    </TabsList>
  </ScrollStrip>

  <!-- The primitive mounts a panel per tab and hides the closed ones, so the
       body is drawn only for the open tab: a closed panel would otherwise
       hold live telemetry or pulse a skeleton nobody is looking at. -->
  {#each tabs as tab (tab.id)}
    <TabsContent value={tab.id}>
      {#if tab.id === active}
        {#if tab.panel}
          {@render tab.panel()}
        {:else if fallback}
          {@render fallback()}
        {:else}
          <div class="flex flex-col gap-4">
            <Skeleton class="h-32 w-full" />
            <Skeleton class="h-4 w-2/3" />
            <Skeleton class="h-4 w-1/2" />
          </div>
        {/if}
      {/if}
    </TabsContent>
  {/each}
</Tabs>
