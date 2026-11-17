<!--
  The trail of names above a page: where this page sits, and one press back to
  each place above it.

  The nav is named, so a screen reader can list it with the page's other
  landmarks and say which one this is.
-->
<script lang="ts">
  import { t } from '../../i18n';
  import type { BreadcrumbProps } from './types';

  let { label, children, ...rest }: BreadcrumbProps = $props();

  // A caller may name its own words. The default is read from the catalogue
  // when the component draws, so a change of language rewrites it
  // (frontend-i18n.md rule 1).
  const labelText = $derived(label ?? t('primitives.breadcrumb.label'));
</script>

<nav aria-label={labelText} data-slot="breadcrumb" {...rest}>
  {@render children?.()}
</nav>
