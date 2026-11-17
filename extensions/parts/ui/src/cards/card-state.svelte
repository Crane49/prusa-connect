<!--
  What a parts card draws before and instead of its parts: a skeleton while
  they are read, the fault by its code when they could not be, and a line
  that says why the card is empty and what to do about it, drawn as the
  host's own cards draw theirs (a glyph, a heading, a quieter note). The
  parts themselves are the caller's.
-->
<script lang="ts">
  import {
    EmptyState,
    FaultAlert,
    Skeleton,
  } from '@prusa/connect-ui/primitives';
  import PackageIcon from '@lucide/svelte/icons/package';
  import type { Snippet } from 'svelte';

  import { faultText } from '../faults';
  import { heldSession } from '../session';
  import type { CardParts } from './card-parts.svelte';

  let {
    parts,
    empty,
    children,
  }: {
    parts: CardParts;
    /** Why the card holds no part, and what a person does about it. */
    empty: { heading: string; note?: string | undefined };
    children: Snippet;
  } = $props();

  const t = heldSession().host.t;
</script>

{#if parts.loading}
  <div
    role="status"
    aria-label={t('parts.listing.loading')}
    class="grid min-h-0 flex-1 grid-cols-2 gap-2"
  >
    <Skeleton class="h-full" />
    <Skeleton class="h-full" />
  </div>
{:else if parts.fault !== undefined}
  <FaultAlert fault={faultText(parts.fault, t)} />
{:else if parts.rows.length === 0}
  <EmptyState
    size="sm"
    headingLevel="h3"
    icon={PackageIcon}
    heading={empty.heading}
    description={empty.note}
    data-part="dashboard-empty"
  />
{:else}
  {@render children()}
{/if}
