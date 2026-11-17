<!-- A module's file view that throws while it renders, for the boundary test. -->
<script lang="ts">
  let { fileRef }: { fileRef: string } = $props();

  const reading = $derived.by((): string => {
    throw new Error(`this view is broken: ${fileRef}`);
  });
</script>

<p data-testid="throwing-file-view">{reading}</p>
