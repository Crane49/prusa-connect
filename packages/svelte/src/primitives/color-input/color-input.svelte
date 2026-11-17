<script lang="ts">
  import type { ColorInputProps } from './types';

  let { value, oninput, label, class: className }: ColorInputProps = $props();
</script>

<input
  type="color"
  aria-label={label}
  {value}
  class={[
    'h-8 w-10 cursor-pointer rounded-sm border border-border bg-transparent p-0.5 focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background focus-visible:outline-none',
    className,
  ]}
  oninput={(e) => oninput(e.currentTarget.value.toUpperCase())}
/>
