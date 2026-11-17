<!-- One person's page: their access to every printer. -->
<script lang="ts">
  import { onMount } from 'svelte';

  import {
    AccessGrants,
    FleetListing,
    readSubjectGrants,
  } from '@prusa/connect-ui/data';
  import { PersonAccessScreen } from '@prusa/connect-ui/shell';

  import { client } from '../client';
  import { PATHS } from '../routes';
  import type { AppContext } from '../screens';

  let { ctx }: { ctx: AppContext } = $props();

  const id = $derived(ctx.params.id ?? '');
  const account = $derived(ctx.accounts.items.find((row) => row.id === id));
  const subject = $derived(account?.subject);

  // Its own page of the fleet, so a search here does not narrow the Printers
  // page the reader comes back to.
  const printers = new FleetListing(client);
  const grants = $derived(
    new AccessGrants(client, (grantsClient, signal) =>
      readSubjectGrants(grantsClient, subject ?? '', signal),
    ),
  );

  onMount(() => {
    void ctx.accounts.load();
    printers.setQuery({ pageSize: 25, sort: 'name', order: 'asc' });

    return () => {
      printers.close();
    };
  });
</script>

<!-- Keyed, so the screen mounts and reads the grants once the accounts
       said who the person is. -->
{#key subject}
  <PersonAccessScreen
    {account}
    accountsLoaded={ctx.accounts.loaded}
    {printers}
    {grants}
    self={ctx.session.principal?.id}
    peopleHref={PATHS.settingsPeople}
  />
{/key}
