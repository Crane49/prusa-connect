<!--
  Draws nothing. Mounted under a key of the period, it asks the store to read
  the period a card shows when the card mounts and when its settings change.
-->
<script lang="ts">
  import { onMount } from 'svelte';

  import type { Days, Stats } from '../data/stats.svelte';

  let { stats, days }: { stats: Stats; days: Days } = $props();

  onMount(() => {
    stats.want(days);
  });
</script>
