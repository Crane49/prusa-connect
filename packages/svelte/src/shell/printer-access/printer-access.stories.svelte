<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { fakeListingStore } from '../../test/fakes';
  import {
    fakeGrants,
    fakePeople,
    fakePrinters,
    grantsOnPrinter,
    people,
    PRINTER,
    SUBJECT,
  } from './access.fixtures';
  import PersonAccessScreen from './person-access-screen.svelte';
  import PrinterAccessCard from './printer-access-card.svelte';

  const { Story } = defineMeta({
    title: 'Screens/Printer Access',
    component: PrinterAccessCard,
  });

  const store = fakeListingStore();
  const ADMINISTER = ['observe', 'operate', 'configure', 'administer'];
  const [, jana, workshop, intern] = people;
</script>

<!--
  The two places one person's access to one printer is changed: the card on
  the printer's page, and the person's own page. Both draw the same row.
-->

<Story name="On The Printer Page" asChild>
  <PrinterAccessCard
    deviceId={PRINTER}
    callerActions={ADMINISTER}
    people={fakePeople()}
    grants={fakeGrants(grantsOnPrinter)}
    self={SUBJECT}
    {store}
  />
</Story>

<Story name="A Change Is In Flight" asChild>
  <PrinterAccessCard
    deviceId={PRINTER}
    callerActions={ADMINISTER}
    people={fakePeople()}
    grants={fakeGrants(grantsOnPrinter, {
      busy: (_device: string, subject: string) => subject === 'user:intern',
    })}
    self={SUBJECT}
    {store}
  />
</Story>

<Story name="Loading" asChild>
  <PrinterAccessCard
    deviceId={PRINTER}
    callerActions={ADMINISTER}
    people={fakePeople()}
    grants={fakeGrants({}, { loaded: false, loading: true })}
    self={SUBJECT}
    {store}
  />
</Story>

<Story name="A Person And Every Printer" asChild>
  <PersonAccessScreen
    account={intern}
    accountsLoaded
    printers={fakePrinters()}
    grants={fakeGrants({ 'user:intern': ['operator'] })}
    self={SUBJECT}
    peopleHref="/settings/people"
  />
</Story>

<Story name="A Grant The Organization Role Outranks" asChild>
  <PersonAccessScreen
    account={workshop}
    accountsLoaded
    printers={fakePrinters()}
    grants={fakeGrants({ 'user:workshop': ['viewer', 'blocked'] })}
    self={SUBJECT}
    peopleHref="/settings/people"
  />
</Story>

<Story name="An Organization Administrator" asChild>
  <PersonAccessScreen
    account={jana}
    accountsLoaded
    printers={fakePrinters()}
    grants={fakeGrants({})}
    self={SUBJECT}
    peopleHref="/settings/people"
  />
</Story>
