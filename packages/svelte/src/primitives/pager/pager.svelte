<!--
  One pager for every paged listing: how many rows a page holds, where in the
  list you are, and the two steps through it.

  It replaces the pager each listing grew a copy of. The page-size control is
  what forced the issue, because a preference duplicated per screen is a
  preference that drifts per screen.

  The size control leads. Reaching for a pager is usually a way of saying show
  me more at once, and the answer to that is the size and not the arrow.

  The sentence between the two says both numbers a reader needs: how many rows
  are in front of them right now, and how many there are in all. A page number
  alone answers neither, since page 2 of 3 over a screen of eleven rows leaves
  the size of the list to arithmetic.
-->
<script lang="ts">
  import { formatNumber, t } from '../../i18n';
  import Button from '../button/button.svelte';
  import Label from '../label/label.svelte';
  import Select from '../select/select.svelte';
  import SelectContent from '../select/select-content.svelte';
  import SelectItem from '../select/select-item.svelte';
  import SelectTrigger from '../select/select-trigger.svelte';
  import SelectValue from '../select/select-value.svelte';
  import { PAGE_SIZES, type PageSize } from './sizes';
  import type { PagerProps } from './types';

  const uid = $props.id();

  let {
    page,
    pageCount,
    pageSize,
    onPageChange,
    onPageSizeChange,
    unit,
    total,
    count,
    class: className,
  }: PagerProps = $props();

  const sizeId = `pager-size-${uid}`;

  const sizes = $derived(
    PAGE_SIZES.map((size) => ({
      value: String(size),
      label: formatNumber(size),
    })),
  );

  // How many rows are in front of the reader now: a full page, or what is
  // left of the list on the last one.
  const shown = $derived(
    Math.max(0, Math.min(pageSize, (total ?? 0) - page * pageSize)),
  );

  const sentence = $derived(
    unit === undefined
      ? t('primitives.pager.statusPlain', {
          page: page + 1,
          pages: pageCount,
          shown,
          total: total ?? 0,
        })
      : t('primitives.pager.status', {
          page: page + 1,
          pages: pageCount,
          shown,
          total: total ?? 0,
          unit,
        }),
  );
</script>

<div class={['flex flex-wrap items-center justify-between gap-3', className]}>
  <div class="flex items-center gap-2">
    <Label for={sizeId} class="text-xs text-muted-foreground">
      {t('primitives.pager.perPage')}
    </Label>
    <Select
      value={String(pageSize)}
      items={sizes}
      onValueChange={(next: string) => {
        onPageSizeChange(Number(next) as PageSize);
      }}
    >
      <!-- `sm`, the size the filters and the view toggle above the listing
           wear. At `lg` this select and the two arrows stood 16px taller than
           every other control and read as the heaviest thing on the page,
           which a pager over a one-page list is not. -->
      <SelectTrigger id={sizeId} size="sm">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {#each sizes as size (size.value)}
          <SelectItem value={size.value} label={size.label}>
            {size.label}
          </SelectItem>
        {/each}
      </SelectContent>
    </Select>
  </div>

  <!-- The rows themselves change silently, so this sentence is the only thing
       telling a screen reader that the page moved. -->
  {#if count}
    {@render count()}
  {:else}
    <p aria-live="polite" class="text-xs font-medium text-muted-foreground">
      {sentence}
    </p>
  {/if}

  <div class="flex items-center gap-2">
    <!-- `secondary` and not `ghost`: a ghost pair reads as plain body text
         until it is hovered, and the primary device has no hover. `sm` for the
         same reason the size control is, so the pager matches the filter row
         rather than out-weighing it. -->
    <Button
      variant="secondary"
      size="sm"
      disabled={page === 0}
      onclick={() => {
        onPageChange(page - 1);
      }}
    >
      {t('primitives.previous')}
    </Button>
    <Button
      variant="secondary"
      size="sm"
      disabled={page >= pageCount - 1}
      onclick={() => {
        onPageChange(page + 1);
      }}
    >
      {t('primitives.next')}
    </Button>
  </div>
</div>
