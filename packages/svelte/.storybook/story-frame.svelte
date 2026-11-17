<!--
  What every story is drawn inside: the theme and the skin on the document
  element. Nothing is wrapped around the story itself, so a screen measures the
  width Storybook says it runs at.

  The theme is a class and an attribute. `tokens.css` selects `.dark`, which is
  what `ThemeToggle` writes on a real page, and `data-theme` is written beside
  it so a reader inspecting the page sees which of the two is on. The page's
  own background and text colour come from the theme's base layer, which
  `preview.css` imports.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';

  import { applySkin } from '../src/styles/skin';

  interface Props {
    /** Whether the dark side of the theme is on. */
    dark: boolean;
    /** Which skin the theme is drawn in. */
    skin: string;
    children?: Snippet;
  }

  const { dark, skin, children }: Props = $props();

  $effect(() => {
    const root = document.documentElement;
    root.classList.toggle('dark', dark);
    root.dataset.theme = dark ? 'dark' : 'light';
  });

  $effect(() => {
    applySkin(skin);
  });
</script>

{@render children?.()}
