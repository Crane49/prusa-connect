<!-- A module page that draws another module's picker and keeps what the
     person picked, for the component registry test. -->
<script lang="ts">
  import type { ComponentUnavailable } from '../slots/host';
  import ModuleComponent from '../slots/module-component/module-component.svelte';
  import { heldHost } from './held-host';

  let {
    module = 'spools',
    name = 'spool-picker',
  }: { module?: string; name?: string } = $props();

  const host = heldHost();
  let picked = $state('nothing');
</script>

<ModuleComponent
  load={() =>
    host.components.load<{
      items: readonly string[];
      onselect: (item: string) => void;
    }>(module, name)}
  props={{
    items: ['PLA', 'PETG'],
    onselect: (item: string) => {
      picked = item;
    },
  }}
>
  {#snippet fallback(why: ComponentUnavailable)}
    <p data-testid="fallback">{why.reason}</p>
  {/snippet}
</ModuleComponent>
<p data-testid="picked">{picked}</p>
