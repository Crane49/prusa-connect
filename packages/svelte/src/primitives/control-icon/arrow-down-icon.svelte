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
    d="m22 8.107-2.356-2.356-7.647 7.781-7.641-7.781L2 8.107l9.997 10.14z"
  />
</svg>
