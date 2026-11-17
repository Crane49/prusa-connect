<!-- The spools module's picker as this module sees it: a component that
     calls onselect with a spool. -->
<script lang="ts">
  let {
    onselect,
  }: { onselect: (spool: { id: string; material: string }) => void } = $props();
</script>

<button
  type="button"
  onclick={() => onselect({ id: 'petg-black', material: 'PETG' })}
>
  petg-black
</button>
