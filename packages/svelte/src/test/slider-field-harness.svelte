<!--
  The caller a `SliderField` needs. The field only drafts a value, so a story
  and a test both need something that owns one and keeps what was committed.
-->
<script lang="ts">
  import { SliderField } from '../primitives/slider-field';

  let { initial = 100 }: { initial?: number } = $props();

  // The harness is created with one starting value and never handed another.
  // svelte-ignore state_referenced_locally
  let value = $state(initial);
</script>

<div class="max-w-sm">
  <SliderField
    {value}
    onCommit={(next: number) => {
      value = next;
    }}
    min={50}
    max={200}
    unit="%"
    label="Speed override"
  />
  <p class="mt-3 text-sm text-muted-foreground">
    Committed: <span data-testid="committed">{value}</span>%
  </p>
</div>
