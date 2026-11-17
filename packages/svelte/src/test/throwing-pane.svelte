<!-- A settings pane that throws while it renders, for the boundary test. -->
<script lang="ts">
  const reading = $derived.by((): string => {
    throw new Error('this pane is broken');
  });
</script>

<p data-testid="throwing-pane">{reading}</p>
