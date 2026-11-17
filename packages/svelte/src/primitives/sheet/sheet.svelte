<!--
  A panel that slides in from an edge of the window. bits-ui carries the open
  state, the focus trap, the outside press, the escape key and the focus
  return.

  A sheet is a dialog in bits-ui, the same as it is in Radix. There is no
  separate sheet primitive, and the difference between the two is the position
  and the animation, which are classes on the content.

  There is no `modal` prop here, where React's root has one. Radix reads
  `modal` on the root and turns three separate things off together: it renders
  no overlay at all, it skips the body scroll lock, and it lets a press fall
  through to the page. React had to hand-write a scrim, a ref-counted
  wheel and touch lock and a context to carry the state to them, because Radix
  offers no way to keep one of the three and drop another. bits-ui has no
  `modal` prop and no such coupling: `Dialog.Overlay` always draws while the
  dialog is open, and `preventScroll`, `trapFocus`, `escapeKeydownBehavior` and
  `interactOutsideBehavior` are separate props on the content. A caller that
  wants the phone tier's behaviour, where the bottom bar stays hittable, passes
  those and needs nothing from this file.

  The root renders no element, so it takes no `data-slot` and no class.
-->
<script lang="ts">
  import { Dialog as SheetPrimitive } from 'bits-ui';

  import type { SheetProps } from './types';

  let { open = $bindable(false), ...rest }: SheetProps = $props();
</script>

<SheetPrimitive.Root bind:open {...rest} />
