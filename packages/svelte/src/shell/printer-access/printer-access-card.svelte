<!--
  The printer page's Access card: every person on this server and what each
  may do on this printer. It is drawn only once the people were read, so a
  caller who may not list them gets no card rather than a card that says no.

  The lists are read when the card mounts and again when the tab comes back
  into view, since the server tells no one that a grant changed.
-->
<script lang="ts">
  import { onMount } from 'svelte';

  import type { AccessChoice } from '../../domain';
  import { t } from '../../i18n';
  import { ListingState, viewModes } from '../../primitives';
  import { browserStore, matches } from '../settings-people/presentation';
  import AccessList from './access-list.svelte';
  import { peopleRowsOf } from './presentation';
  import type { AccessRow, PrinterAccessCardProps } from './types';

  let {
    deviceId,
    callerActions,
    people,
    grants,
    self,
    store = browserStore(),
  }: PrinterAccessCardProps = $props();

  let search = $state('');

  const rows = $derived(
    peopleRowsOf(
      people.items.filter((account) =>
        matches(search, account.username, account.display_name),
      ),
      grants,
      deviceId,
      callerActions,
      self,
    ),
  );

  const listing = new ListingState<AccessRow>({
    id: 'printer-access',
    fields: [],
    views: viewModes(['rows']),
    initialView: 'rows',
    store: {
      read: (key) => store.read(key),
      write: (key, value) => store.write(key, value),
    },
    rows: () => rows,
  });

  const shown = $derived(people.loaded && people.error === undefined);

  const reload = (): void => {
    void people.load();
    void grants.load();
  };

  onMount(() => {
    reload();
    const onVisible = (): void => {
      if (document.visibilityState === 'visible') reload();
    };
    document.addEventListener('visibilitychange', onVisible);

    return () => {
      document.removeEventListener('visibilitychange', onVisible);
    };
  });
</script>

{#if shown}
  <AccessList
    title={t('printerAccess.title')}
    description={t('printerAccess.description')}
    rows={listing.page}
    waiting={!grants.loaded && grants.error === undefined}
    fault={grants.error}
    {search}
    onSearch={(next: string) => {
      search = next;
    }}
    searchLabel={t('printerAccess.searchPeople')}
    empty={{
      heading: t('printerAccess.emptyPeople.heading'),
      note: t('printerAccess.emptyPeople.note'),
    }}
    pager={listing.pager === null
      ? undefined
      : { ...listing.pager, total: listing.total }}
    onOn={(row: AccessRow, on: boolean) => {
      void grants.setOn(row.deviceId, row.subject, row.orgRole, on);
    }}
    onChoice={(row: AccessRow, choice: AccessChoice) => {
      void grants.setChoice(row.deviceId, row.subject, choice);
    }}
  />
{/if}
