<!--
  The stack of toasts a page raises. svelte-sonner does the stacking, the
  timing and the announcement. This file sets the surface, the radius and the
  five glyphs, so a toast looks like the rest of the product.

  The theme is read from the `dark` class on the root element. There is no
  theme framework in this package, and `theme` has to be set either way:
  sonner's own `[data-sonner-theme]` rules fall back to `light` when the
  attribute is missing.
-->
<script lang="ts">
  import CircleCheckIcon from '@lucide/svelte/icons/circle-check';
  import InfoIcon from '@lucide/svelte/icons/info';
  import OctagonXIcon from '@lucide/svelte/icons/octagon-x';
  import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';
  import { Toaster as Sonner } from 'svelte-sonner';

  import { t } from '../../i18n';
  import Spinner from '../spinner/spinner.svelte';
  import type { ToasterProps } from './types';

  // sonner's own 4 s is too short to read a toast with a description in it,
  // such as a command result with its time or a reason. sonner also runs the
  // timer down in a background browser tab unless told to pause it, so a
  // result that lands while the person is elsewhere would never be seen.
  let {
    duration = 8000,
    pauseWhenPageIsHidden = true,
    ...rest
  }: ToasterProps = $props();

  // svelte-sonner names the stack and the close button in English of its own,
  // and both reach the screen as an `aria-label` (frontend-i18n.md rule 1).
  const stackLabel = $derived(t('primitives.toaster.label'));
  const closeToastLabel = $derived(t('primitives.toaster.close'));

  let theme = $state<'light' | 'dark'>('light');

  // The read is in an effect because a library component may render on a
  // server, where there is no document (frontend-structure.md). The observer
  // is disconnected on unmount, otherwise it outlives the component
  // (svelte-testing.md).
  $effect(() => {
    const root = document.documentElement;
    const read = (): void => {
      theme = root.classList.contains('dark') ? 'dark' : 'light';
    };

    read();
    const observer = new MutationObserver(read);
    observer.observe(root, { attributes: true, attributeFilter: ['class'] });
    return () => {
      observer.disconnect();
    };
  });
</script>

<!--
  The four custom properties are the values sonner's own stylesheet reads for
  the toast surface, so a toast is painted from this theme. They are written
  inline because sonner sets its own defaults on the same element and an
  inline value is what beats them (frontend-css.md allows a custom property
  that passes a value into the styles). `--border-radius` reads `--radius-sm`
  rather than `--radius`, which this theme does not declare.

  There is no `--width` here. svelte-sonner writes that one itself with a
  `style:` directive, which no style attribute can beat, and its own
  `max-width: 600px` block already makes the toast fit a narrow screen.
-->
<Sonner
  {theme}
  class="toaster group"
  {duration}
  {pauseWhenPageIsHidden}
  containerAriaLabel={stackLabel}
  closeButtonAriaLabel={closeToastLabel}
  style="--normal-bg: var(--popover); --normal-text: var(--popover-foreground); --normal-border: var(--border); --border-radius: var(--radius-sm)"
  {...rest}
>
  {#snippet successIcon()}
    <CircleCheckIcon class="size-4" />
  {/snippet}
  {#snippet infoIcon()}
    <InfoIcon class="size-4" />
  {/snippet}
  {#snippet warningIcon()}
    <TriangleAlertIcon class="size-4" />
  {/snippet}
  {#snippet errorIcon()}
    <OctagonXIcon class="size-4" />
  {/snippet}
  {#snippet loadingIcon()}
    <Spinner />
  {/snippet}
</Sonner>
