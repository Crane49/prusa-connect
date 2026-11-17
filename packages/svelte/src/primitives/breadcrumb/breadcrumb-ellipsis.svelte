<!--
  Stands for the crumbs a long trail leaves out.

  The mark is hidden from a screen reader, the same way the separator is. The
  word under it is for a caller that unhides it, or wraps it in a menu that
  opens the folded crumbs.
-->
<script lang="ts">
  import EllipsisIcon from '@lucide/svelte/icons/ellipsis';

  import { t } from '../../i18n';
  import type { BreadcrumbEllipsisProps } from './types';

  let {
    moreText,
    class: className,
    ...rest
  }: BreadcrumbEllipsisProps = $props();

  // A caller may name its own words. The default is read from the catalogue
  // when the component draws, so a change of language rewrites it
  // (frontend-i18n.md rule 1).
  const moreWords = $derived(moreText ?? t('primitives.breadcrumb.more'));
</script>

<span
  data-slot="breadcrumb-ellipsis"
  role="presentation"
  aria-hidden="true"
  class={['flex size-9 items-center justify-center', className]}
  {...rest}
>
  <EllipsisIcon class="size-4" />
  <span class="sr-only">{moreWords}</span>
</span>
