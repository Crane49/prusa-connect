<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import {
    MARKETPLACE,
    MARKETPLACE_UNREAD,
  } from '../../../../test/extension-fixtures';
  import MarketplaceBadge from './marketplace-badge.svelte';

  const { Story } = defineMeta({
    title: 'Parts/Extensions/Marketplace Badge',
    component: MarketplaceBadge,
  });
</script>

<Story name="Not Configured" asChild>
  <MarketplaceBadge marketplace={undefined} />
</Story>

<Story name="Switched Off" asChild>
  <MarketplaceBadge marketplace={{ ...MARKETPLACE, enabled: false }} />
</Story>

<Story name="Reachable" asChild>
  <MarketplaceBadge marketplace={MARKETPLACE} />
</Story>

<Story name="Unreachable" asChild>
  <MarketplaceBadge marketplace={MARKETPLACE_UNREAD} />
</Story>

<Story name="Unreachable With A Stale Copy" asChild>
  <MarketplaceBadge
    marketplace={{ ...MARKETPLACE, reachable: false, stale: true }}
  />
</Story>
