<!--
  A crumb a person can press to go back up the trail.

  The focus ring is the Button's recipe, so a keyboard walk across a page rings
  every interactive element the same way.

  A caller that needs its own element, a router link or a button, passes the
  `child` snippet and spreads the attributes it receives.
-->
<script lang="ts">
  import type { BreadcrumbLinkProps } from './types';

  let {
    child,
    children,
    class: className,
    ...rest
  }: BreadcrumbLinkProps = $props();

  const classes =
    'rounded-sm transition-colors outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background';
</script>

{#if child}
  {@render child({
    props: {
      ...rest,
      'data-slot': 'breadcrumb-link',
      class: [classes, className],
    },
  })}
{:else}
  <a data-slot="breadcrumb-link" class={[classes, className]} {...rest}>
    {@render children?.()}
  </a>
{/if}
