<script lang="ts">
  import type { ControlIconProps } from './types';
  import { base } from './variants';

  let { class: className, ...rest }: ControlIconProps = $props();
</script>

<svg
  viewBox="0 0 24 24"
  xmlns="http://www.w3.org/2000/svg"
  data-slot="control-icon"
  class={[base, className]}
  {...rest}
>
  <path
    fill="currentColor"
    d="m2 15.89 2.358 2.358 7.646-7.782 7.639 7.782 2.358-2.358-9.997-10.14z"
  />
</svg>
