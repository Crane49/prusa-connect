<!--
  The timer every tooltip under it shares, so moving between two controls shows
  the second one at once instead of waiting again.

  It renders no element. `delayDuration` is zero: a tooltip in this product
  names an icon-only control, and a name that arrives after a pause is a name
  the reader has already gone looking for elsewhere.

  `ignoreNonKeyboardFocus` is on, so only focus from the keyboard opens a
  tooltip. A dialog or a menu gives focus back to its trigger when it closes,
  and a tooltip must not appear on the button the person just used.
-->
<script lang="ts">
  import { Tooltip as TooltipPrimitive } from 'bits-ui';

  import type { TooltipProviderProps } from './types';

  let {
    delayDuration = 0,
    ignoreNonKeyboardFocus = true,
    ...rest
  }: TooltipProviderProps = $props();
</script>

<TooltipPrimitive.Provider {delayDuration} {ignoreNonKeyboardFocus} {...rest} />
