<!-- A camera section that, like a module feeding only some cameras, draws
     nothing for a camera it does not know. -->
<script lang="ts">
  let { camera }: { camera: string } = $props();
</script>

{#if camera === 'bed-cam'}
  <p>status of {camera}</p>
{/if}
