<!--
  What a list says when it holds nothing: a drawing, a heading, a line of
  explanation and the one thing a person can do about it.

  `size="sm"` is the card-sized version: it fills its column and draws a
  smaller glyph and type, with the action directly under the note.

  The heading tag comes from the caller, so the message lands at the right
  depth in whatever page it sits on.
-->
<script lang="ts">
  import type { EmptyStateProps } from './types';

  let {
    icon: Icon,
    heading,
    headingLevel,
    description,
    action,
    size = 'md',
    class: className,
    ...rest
  }: EmptyStateProps = $props();
</script>

<div
  data-slot="empty-state"
  class={[
    'flex flex-col items-center text-center',
    size === 'sm' ? 'flex-1 justify-center gap-1 p-3' : 'gap-2 px-6 py-12',
    className,
  ]}
  {...rest}
>
  {#if Icon}
    <Icon
      aria-hidden="true"
      class={[
        'text-muted-foreground',
        size === 'sm' ? 'size-6' : 'mb-2 size-10',
      ]}
    />
  {/if}
  <svelte:element
    this={headingLevel ?? 'h3'}
    class={size === 'sm' ? 'text-sm font-medium' : 'text-base font-semibold'}
  >
    {heading}
  </svelte:element>
  {#if description}
    <p
      class={[
        'text-muted-foreground',
        size === 'sm' ? 'text-xs' : 'max-w-sm text-sm',
      ]}
    >
      {description}
    </p>
  {/if}
  {#if action}
    {#if size === 'sm'}
      {@render action()}
    {:else}
      <div class="mt-2">{@render action()}</div>
    {/if}
  {/if}
</div>
