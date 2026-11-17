<!--
  A button drawn at the size of a heading: the name of the thing a person is
  looking at, with a glyph that says it can be changed.

  The heading element stays the caller's; wrap this in it. Rest props are
  spread, so a DropdownMenuTrigger's `child` props land on the button.
-->
<script lang="ts">
  import type { TitleButtonProps } from './types';

  let {
    level = 'page',
    icon: Icon,
    label,
    class: className,
    children,
    ...rest
  }: TitleButtonProps = $props();
</script>

<button
  type="button"
  data-slot="title-button"
  aria-label={label}
  class={[
    'flex max-w-full min-w-0 cursor-pointer items-center gap-2 rounded-sm font-semibold transition-colors hover:bg-accent focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background focus-visible:outline-none disabled:pointer-events-none disabled:text-disabled-foreground',
    level === 'page' ? 'px-2 py-1 text-2xl' : 'px-1 text-lg',
    className,
  ]}
  {...rest}
>
  <span class="truncate">{@render children()}</span>
  <Icon
    aria-hidden="true"
    class={[
      'shrink-0 text-muted-foreground',
      level === 'page' ? 'size-5' : 'size-4',
    ]}
  />
</button>
