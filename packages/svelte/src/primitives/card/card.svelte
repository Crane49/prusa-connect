<!--
  A bordered surface holding one thing: a printer, a job, a group of settings.

  Padding is 16px until the card itself clears 24rem, and 24px above that. On a
  320px screen the page's own 16px inset plus 24px of card each side left the
  content 240px wide, a third of the width spent on air. The card names its own
  container, so the step keys off the card's width and not the viewport's: a
  card in a narrow column on a wide screen has the same problem.
-->
<script lang="ts">
  import type { CardProps } from './types';

  let {
    tone = 'default',
    density = 'default',
    class: className,
    children,
    ...rest
  }: CardProps = $props();
</script>

<div
  data-slot="card"
  data-tone={tone}
  class={[
    '@container/card flex flex-col rounded-sm border bg-card text-card-foreground',
    density === 'default' &&
      'gap-4 py-4 @[24rem]/card:gap-6 @[24rem]/card:py-6',
    density === 'compact' && 'gap-3 py-4',
    density === 'tight' && 'gap-1 py-2',
    density === 'flush' && 'gap-0 py-0',
    tone === 'default' && 'border-border',
    tone === 'critical' && 'border-destructive bg-destructive/5',
    tone === 'attention' && 'border-warning bg-warning/5',
    className,
  ]}
  {...rest}
>
  {@render children?.()}
</div>
