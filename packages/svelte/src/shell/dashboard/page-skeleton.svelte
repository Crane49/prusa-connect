<!--
  The page's own shape while the first answer is on its way: the head, and
  cards of the sizes a new dashboard has, so nothing jumps when it lands.
-->
<script lang="ts">
  import { t } from '../../i18n';
  import { Skeleton } from '../../primitives';
</script>

<div role="status" aria-label={t('dashboard.loading')} class="grid gap-4">
  <div class="flex items-center justify-between gap-4">
    <Skeleton class="h-9 w-64" />
    <Skeleton class="h-9 w-48" />
  </div>
  <div class="grid grid-cols-12 gap-3">
    <Skeleton class="col-span-12 h-28" />
    <Skeleton class="col-span-8 h-80" />
    <Skeleton class="col-span-4 h-80" />
  </div>
</div>
