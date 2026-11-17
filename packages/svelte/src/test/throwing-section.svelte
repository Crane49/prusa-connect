<!-- A device-card section that throws while it renders, for the boundary test.
     It throws inside a derived so that the failure happens during the render
     the boundary is watching. -->
<script lang="ts">
  let { deviceId }: { deviceId: string } = $props();

  const reading = $derived.by((): string => {
    throw new Error(`this section is broken: ${deviceId}`);
  });
</script>

<p data-testid="throwing-section">{reading}</p>
