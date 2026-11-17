<!--
  The page's own shape while the frame is on its way: the header, the tree
  beside the listing, and rows of the height the cards will have, so nothing
  jumps when the answer lands.
-->
<script lang="ts">
  import { Card, CardContent, Skeleton } from '@prusa/connect-ui/primitives';

  import { heldSession } from '../session';
  import { SPLIT_LEADING, SPLIT_ROW, SPLIT_TRAILING } from './layout';

  const t = heldSession().host.t;
</script>

<div role="status" aria-label={t('parts.loading')} class="grid gap-y-4">
  <Skeleton class="h-8 w-48" />
  <Card>
    <CardContent class={SPLIT_ROW}>
      <div class={SPLIT_LEADING}>
        {#each { length: 6 }, row (row)}
          <Skeleton class="h-6 w-full" />
        {/each}
      </div>
      <div
        class={[
          SPLIT_TRAILING,
          'grid grid-cols-2 gap-3 @2xl/parts:grid-cols-4',
        ]}
      >
        {#each { length: 8 }, card (card)}
          <Skeleton class="aspect-square w-full" />
        {/each}
      </div>
    </CardContent>
  </Card>
</div>
