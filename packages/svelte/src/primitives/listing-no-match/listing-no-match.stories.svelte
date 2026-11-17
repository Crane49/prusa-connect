<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { t } from '../../i18n';
  import ListingNoMatch from './listing-no-match.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/ListingNoMatch',
    component: ListingNoMatch,
    args: {
      heading: 'No printers match',
      headingLevel: 'h2',
      description: 'Try a different search or fewer filters.',
      onClear: () => undefined,
    },
  });
</script>

<Story name="Default" />

<Story
  name="Search Only"
  args={{ clearLabel: t('primitives.listing.clearSearch') }}
/>
