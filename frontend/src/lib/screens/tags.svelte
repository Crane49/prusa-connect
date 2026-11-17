<!--
  This product's tags page: the library's screen, fed this product's fleet.
  Each tag links to the printers list filtered to that tag.
-->
<script lang="ts">
  import { TagsScreen } from '@prusa/connect-ui/shell';

  import { printersTaggedHref } from '../routes';
  import type { AppContext } from '../screens';

  let { ctx }: { ctx: AppContext } = $props();

  const fleet = $derived(ctx.fleet);

  $effect(() => {
    void fleet.load();
  });
</script>

<TagsScreen {fleet} printersHref={(tag) => printersTaggedHref([tag])} />
