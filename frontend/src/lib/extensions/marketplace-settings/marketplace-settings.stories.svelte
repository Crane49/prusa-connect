<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import {
    catalogueOf,
    MARKETPLACE,
    MARKETPLACE_UNREAD,
  } from '../../../../test/extension-fixtures';
  import MarketplaceSettings from './marketplace-settings.svelte';

  const { Story } = defineMeta({
    title: 'Parts/Extensions/Marketplace Settings',
    component: MarketplaceSettings,
  });
</script>

<Story name="Not Configured" asChild>
  <MarketplaceSettings
    id="marketplace"
    catalogue={catalogueOf({ marketplace: undefined })}
  />
</Story>

<Story name="Switched On" asChild>
  <MarketplaceSettings id="marketplace" catalogue={catalogueOf()} />
</Story>

<Story name="Community Extensions Listed" asChild>
  <MarketplaceSettings
    id="marketplace"
    catalogue={catalogueOf({
      marketplace: { ...MARKETPLACE, community: true },
    })}
  />
</Story>

<Story name="Switched Off" asChild>
  <MarketplaceSettings
    id="marketplace"
    catalogue={catalogueOf({
      marketplace: { ...MARKETPLACE, enabled: false },
    })}
  />
</Story>

<Story name="Unreachable" asChild>
  <MarketplaceSettings
    id="marketplace"
    catalogue={catalogueOf({
      marketplace: {
        ...MARKETPLACE_UNREAD,
        problem: 'dial tcp: lookup marketplace.example: no such host',
      },
    })}
  />
</Story>

<Story name="Unreachable With A Stale Copy" asChild>
  <MarketplaceSettings
    id="marketplace"
    catalogue={catalogueOf({
      marketplace: {
        ...MARKETPLACE,
        reachable: false,
        stale: true,
        problem: 'dial tcp: lookup marketplace.example: no such host',
      },
    })}
  />
</Story>

<Story name="Only An Organization Administrator May Switch" asChild>
  <MarketplaceSettings
    id="marketplace"
    catalogue={catalogueOf({ mayAdministerOrg: false })}
  />
</Story>

<Story name="Switch Refused" asChild>
  <MarketplaceSettings
    id="marketplace"
    catalogue={catalogueOf({ switchError: new Error('refused') })}
  />
</Story>

<Story name="Waiting For Another Act" asChild>
  <MarketplaceSettings
    id="marketplace"
    catalogue={catalogueOf({ switching: true })}
  />
</Story>
