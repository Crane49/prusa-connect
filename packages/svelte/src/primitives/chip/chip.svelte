<!--
  A small pill that narrows or opens something: a tag, a filter. It is a link
  when given an href, a toggle button otherwise.
-->
<script lang="ts">
  import type { ChipPressProps, ChipProps } from './types';

  let {
    href,
    onclick,
    selected = false,
    count,
    label,
    child,
    children,
    class: className,
  }: ChipProps = $props();

  const classes = $derived([
    'inline-flex items-center gap-1 rounded-xs border px-1.5 py-0.5 text-xs outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background',
    selected
      ? 'border-selected bg-selected text-selected-foreground'
      : 'border-border text-muted-foreground hover:bg-accent hover:text-foreground',
    className,
  ]);

  const attrs: ChipPressProps = $derived({
    class: classes,
    'aria-label': label,
    ...(href !== undefined || child
      ? { 'aria-current': selected ? ('page' as const) : undefined }
      : { 'aria-pressed': selected }),
  });
</script>

{#snippet content()}
  {@render children?.()}
  {#if count !== undefined}
    <span class="tabular-nums">{count}</span>
  {/if}
{/snippet}

{#if child}
  {@render child({ props: attrs })}
{:else if href !== undefined}
  <a data-slot="chip" {href} {onclick} {...attrs}>{@render content()}</a>
{:else}
  <button data-slot="chip" type="button" {onclick} {...attrs}>
    {@render content()}
  </button>
{/if}
