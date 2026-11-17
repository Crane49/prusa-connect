<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import ListingSearchSummary from './listing-search-summary.svelte';
  import ListingSearch from './listing-search.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/Listing Search',
    component: ListingSearch,
    args: {
      value: '',
      onValue: () => undefined,
      label: 'Search printers',
      placeholder: 'Search printers',
    },
  });
</script>

<Story name="Empty" />

<Story name="With A Query" args={{ value: 'MK4' }} />

<Story
  name="Commit On Enter"
  args={{ value: 'Original Prusa MK4S', commit: 'change' }}
/>

<Story name="Capped Length" args={{ value: 'MK4', maxlength: 8 }} />

<!-- The caller's class sizes the whole box, so the X stays inside a box
     narrower than its row. -->
<Story
  name="Narrower Than Its Row"
  args={{ value: 'MK4', class: 'max-w-sm' }}
/>

<Story name="Summary With Clear" asChild>
  <ListingSearchSummary
    summary="3 printers match MK4"
    clearLabel="Show all"
    onClear={() => undefined}
  />
</Story>
