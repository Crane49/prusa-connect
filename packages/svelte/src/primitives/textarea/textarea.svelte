<!--
  A box for text over several lines.

  `field-sizing-content` makes the box as tall as its text, so a person has no
  reason to drag it taller. `resize-none` removes the handle.

  A disabled box greys its text instead of fading the whole element, and
  `pointer-events-none` keeps a click from putting a caret in it. The box has
  no fill to grey: `bg-transparent`.

  It paints nothing for an invalid value. `Field` adds the `aria-invalid:*`
  classes when it has an error to show, so a control wired by hand and a
  control inside a field cannot drift apart.

  The focus ring is the Button's recipe, so a keyboard walk across a form rings
  every control the same way.
-->
<script lang="ts">
  import type { TextareaProps } from './types';

  let {
    value = $bindable(),
    class: className,
    ...rest
  }: TextareaProps = $props();
</script>

<textarea
  bind:value
  data-slot="textarea"
  class={[
    'flex field-sizing-content min-h-16 w-full resize-none rounded-sm border border-input bg-transparent px-3 py-2 text-base transition-colors outline-none placeholder:text-muted-foreground disabled:pointer-events-none disabled:text-disabled-foreground md:text-sm',
    'focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background',
    className,
  ]}
  {...rest}></textarea>
