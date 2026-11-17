<!-- A component one module hands others: a list to pick from, which reports
     the pick through a callback prop. -->
<script lang="ts">
  let {
    items,
    onselect,
  }: { items: readonly string[]; onselect: (item: string) => void } = $props();
</script>

<ul data-testid="picker">
  {#each items as item (item)}
    <li>
      <button type="button" onclick={() => onselect(item)}>{item}</button>
    </li>
  {/each}
</ul>
