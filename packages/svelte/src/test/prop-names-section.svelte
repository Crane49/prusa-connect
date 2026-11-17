<!-- A section that prints the names of the props it was handed, for the slot tests. -->
<script lang="ts">
  const props: Record<string, unknown> = $props();
</script>

<p data-testid="prop-names">{Object.keys(props).sort().join(',')}</p>
