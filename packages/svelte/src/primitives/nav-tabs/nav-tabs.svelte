<!--
  Tabs that are routes: each is a link, and the open one is the page the
  router is on. It looks like `DetailTabs`, so a route tab and a state tab are
  indistinguishable to the eye.

  The trigger and list classes are a copy of `TabsTrigger`'s and `TabsList`'s.
  They cannot be one string: the active mark keys off `aria-current` here and
  `data-state` there. Change one, change the other.
-->
<script lang="ts">
  import ScrollStrip from '../detail-tabs/scroll-strip.svelte';
  import type { NavTabsProps } from './types';

  let { label, tabs, active, class: className }: NavTabsProps = $props();
</script>

<nav aria-label={label} class={className}>
  <ScrollStrip {active} {tabs}>
    <ul class="flex gap-6 border-b border-border">
      {#each tabs as tab (tab.id)}
        <li>
          <a
            href={tab.href}
            aria-current={active === tab.id ? 'page' : undefined}
            class={[
              'inline-flex min-h-11 items-center justify-center gap-2 border-b-2 px-1 pb-2 text-sm font-medium tracking-wider whitespace-nowrap uppercase transition-colors outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background',
              active === tab.id
                ? 'border-selected text-foreground'
                : 'border-transparent text-muted-foreground hover:text-foreground',
            ]}
          >
            {tab.label}
          </a>
        </li>
      {/each}
    </ul>
  </ScrollStrip>
</nav>
