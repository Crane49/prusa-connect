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
    d="M8.107 2 5.751 4.356l7.781 7.647-7.781 7.642L8.107 22l10.14-9.997z"
  />
</svg>
