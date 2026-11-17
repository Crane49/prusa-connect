<!--
  A file name that gives up its middle instead of its end.

  Every name this product lists means something at the end.
  `Bracket_Left_Reinforced_v4_FINAL.gcode` and
  `Bracket_Left_Reinforced_v6_verified.gcode` differ only in the last dozen
  characters, and the extension that says whether a row can be printed at all
  is the very last thing. A plain `truncate` removes exactly that, which is how
  a jobs row at 390px ends up reading `Bracket_Left_R…`.

  CSS has no middle ellipsis, so the name is two elements: a head that shrinks
  and clips, and a tail beside it that never shrinks. Both are plain text in
  one line box, so a person selects and copies the whole name in one gesture
  and a screen reader reads it once, without a break.
-->
<script lang="ts">
  import type { ClippedNameProps } from './types';

  // Twelve characters is `_v4_FINAL.gcode` minus a little: enough for a
  // version suffix and an extension, which is what tells two names with the
  // same stem apart. Longer than that, and the head has nothing left at a
  // phone width. Shorter, and the extension alone fills the tail.
  const TAIL_LENGTH = 12;

  let {
    name,
    tailLength = TAIL_LENGTH,
    class: className,
  }: ClippedNameProps = $props();

  const head = $derived(name.slice(0, name.length - tailLength));
  const tail = $derived(name.slice(name.length - tailLength));
</script>

{#if name.length <= tailLength}
  <!-- A name that already fits its own tail has no middle to give up. -->
  <span data-slot="clipped-name" class={className}>{name}</span>
{:else}
  <!-- `title` sits on the wrapper and not on the head, so a pointer resting
       anywhere on the name offers the whole of it.

       The head and the tail touch. A newline between the two spans is a text
       node, and the name would read with a space in its middle. -->
  <span
    data-slot="clipped-name"
    title={name}
    class={['flex min-w-0 items-baseline', className]}
    ><span class="min-w-0 truncate">{head}</span><span class="shrink-0"
      >{tail}</span
    ></span
  >
{/if}
