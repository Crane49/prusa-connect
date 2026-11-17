<!-- The bound note field the textarea tests type into. -->
<script lang="ts">
  import { Textarea } from '../primitives/textarea';

  let { value = $bindable('') }: { value?: string } = $props();
</script>

<Textarea bind:value aria-label="Printer notes" />
<output>{value}</output>
