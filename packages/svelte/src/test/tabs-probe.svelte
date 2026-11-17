<!-- A set of tabs with one panel each, for the tabs tests. -->
<script lang="ts">
  import Tabs from '../primitives/tabs/tabs.svelte';
  import TabsContent from '../primitives/tabs/tabs-content.svelte';
  import TabsList from '../primitives/tabs/tabs-list.svelte';
  import TabsTrigger from '../primitives/tabs/tabs-trigger.svelte';

  interface Panel {
    value: string;
    label: string;
    body: string;
    disabled?: boolean;
  }

  let {
    panels,
    value = $bindable(''),
    onValueChange = () => undefined,
  }: {
    panels: readonly Panel[];
    value?: string;
    onValueChange?: (value: string) => void;
  } = $props();
</script>

<Tabs bind:value {onValueChange}>
  <TabsList>
    {#each panels as panel (panel.value)}
      <TabsTrigger value={panel.value} disabled={panel.disabled}>
        {panel.label}
      </TabsTrigger>
    {/each}
  </TabsList>
  {#each panels as panel (panel.value)}
    <TabsContent value={panel.value}>{panel.body}</TabsContent>
  {/each}
</Tabs>
