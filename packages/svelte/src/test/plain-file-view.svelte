<!-- A module's file view that works, for the file-view tests. -->
<script lang="ts">
  let { fileRef }: { fileRef: string } = $props();
</script>

<p data-testid="plain-file-view">module view of {fileRef}</p>
