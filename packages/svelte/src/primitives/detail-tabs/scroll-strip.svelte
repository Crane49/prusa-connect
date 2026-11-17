<!--
  A horizontally scrolling strip of tabs. It scrolls rather than wraps, because
  a wrapped strip moves the panel down the page as the window narrows. The
  edges fade while there is more to scroll to, which is the only sign a narrow
  strip gives that it is not the whole set.

  The scroller is a plain overflow box, the way `Table` holds a wide table:
  `ScrollArea` exposes no handle on its viewport, and the edges need one.

  The open tab is found in the DOM (`data-state="active"` or
  `aria-current="page"`), so one strip serves state tabs and route tabs. It
  is re-found whenever `active` or the set of tabs changes.
-->
<script lang="ts">
  import type { Snippet } from 'svelte';

  let {
    active,
    tabs,
    children,
  }: { active: string; tabs: readonly { id: string }[]; children: Snippet } =
    $props();

  let viewport = $state<HTMLDivElement | null>(null);
  let strip = $state<HTMLDivElement | null>(null);
  let before = $state(false);
  let after = $state(false);

  const measure = (): void => {
    if (viewport === null) return;
    const room = viewport.scrollWidth - viewport.clientWidth;
    before = viewport.scrollLeft > 1;
    after = room - viewport.scrollLeft > 1;
  };

  $effect(() => {
    const element = viewport;
    if (element === null) return;

    measure();
    element.addEventListener('scroll', measure, { passive: true });
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    if (strip !== null) observer.observe(strip);

    return () => {
      element.removeEventListener('scroll', measure);
      observer.disconnect();
    };
  });

  // A tab reached by the path may start off screen in a narrow strip. Only
  // the strip scrolls: `scrollIntoView` would also move the page to a strip
  // drawn further down, such as one inside a module's view.
  $effect(() => {
    // `active` and the ids are read so the effect re-runs when the open tab
    // changes or a tab is added, removed or moved.
    void tabs.map((tab) => tab.id).join();
    if (!active || viewport === null) return;
    const trigger = strip?.querySelector<HTMLElement>(
      '[data-state="active"], [aria-current="page"]',
    );
    if (!trigger) return;
    const left = trigger.offsetLeft;
    const right = left + trigger.offsetWidth;
    if (left < viewport.scrollLeft) viewport.scrollLeft = left;
    else if (right > viewport.scrollLeft + viewport.clientWidth) {
      viewport.scrollLeft = right - viewport.clientWidth;
    }
  });
</script>

<div class="relative">
  <div bind:this={viewport} class="overflow-x-auto">
    <div bind:this={strip} class="w-max">
      {@render children()}
    </div>
  </div>
  {#if before}
    <div
      data-slot="tab-strip-edge"
      aria-hidden="true"
      class="pointer-events-none absolute inset-y-0 left-0 w-8 bg-linear-to-r from-background to-transparent"
    ></div>
  {/if}
  {#if after}
    <div
      data-slot="tab-strip-edge"
      aria-hidden="true"
      class="pointer-events-none absolute inset-y-0 right-0 w-8 bg-linear-to-l from-background to-transparent"
    ></div>
  {/if}
</div>
