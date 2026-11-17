<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import PlainPage from '../../test/plain-page.svelte';
  import ThrowingPane from '../../test/throwing-pane.svelte';
  import { SlotRegistry } from '../registry.svelte';
  import ModulePage from './module-page.svelte';

  const { Story } = defineMeta({
    title: 'Parts/Module Page',
    component: ModulePage,
  });

  const registryWith = (
    component: Parameters<SlotRegistry['registerPage']>[2],
  ): SlotRegistry => {
    const registry = new SlotRegistry();
    registry.declare('spools', {
      pages: [{ path: 'spools', title: 'Spools', hidden: false }],
    });
    registry.registerPage('spools', 'spools', component);

    return registry;
  };
</script>

{#snippet missing()}
  <p class="text-sm text-muted-foreground">No module claims this page.</p>
{/snippet}

<Story name="Claimed By A Module" asChild>
  <ModulePage
    namespace="spools"
    path="spools"
    registry={registryWith(PlainPage)}
  />
</Story>

<Story name="No Module Claims It" asChild>
  <ModulePage
    namespace="spools"
    path="spools"
    registry={new SlotRegistry()}
    fallback={missing}
  />
</Story>

<Story name="Module Page Throws" asChild>
  <ModulePage
    namespace="spools"
    path="spools"
    registry={registryWith(ThrowingPane)}
  />
</Story>
