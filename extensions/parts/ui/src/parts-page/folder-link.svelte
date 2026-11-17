<!--
  A link to one place in the catalogue.

  It is a real link, so a person can open a folder or a part in a new tab, and
  a plain click is taken here and moves the page without a reload. A click
  with a modifier held, or a middle click, is left to the browser, which is
  what a person holding one asks for.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { ClassValue } from 'svelte/elements';

  import type { Spot } from '../navigation.svelte';
  import { heldSession } from '../session';

  let {
    spot,
    class: className,
    current = false,
    label,
    children,
    ...rest
  }: {
    spot: Spot;
    class?: ClassValue | undefined;
    /** The link is to the place on screen, which a screen reader announces. */
    current?: boolean;
    label?: string | undefined;
    children: Snippet;
    /** What a Tile or Chip `child` passes on: its slot, ARIA and `aria-*`. */
    [attribute: string]: unknown;
  } = $props();

  const { navigation } = heldSession();

  const take = (event: MouseEvent): void => {
    if (event.defaultPrevented || event.button !== 0) return;
    if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey)
      return;
    event.preventDefault();
    navigation.go(spot);
  };
</script>

<a
  {...rest}
  href={navigation.href(spot)}
  class={className}
  aria-current={current ? 'page' : undefined}
  aria-label={label}
  onclick={take}
>
  {@render children()}
</a>
