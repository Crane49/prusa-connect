<script module lang="ts">
  import type { ResourceInstance } from '@prusa/connect-ui/data';
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import {
    catalogueOf,
    inState,
    noop,
    pendingOf,
    rowOf,
  } from '../../../../test/extension-fixtures';
  import { longPlan } from '../extension-plan/fixtures';
  import ExtensionDialog from './extension-dialog.svelte';

  const { Story } = defineMeta({
    title: 'Parts/Extensions/Extension Dialog',
    component: ExtensionDialog,
  });

  const catalogue = catalogueOf();
  const known = rowOf();
  const unread = rowOf({ requests_known: false });
  const withPorts = rowOf({
    listen: [
      { name: 'web', network: 'tcp', port: 8080, why: 'serves its own page' },
    ],
  });
  const withDatabase = rowOf({ database: true });
  const installed = rowOf({ version: '1.5.0' }, {});
  const rows = [known];

  const MIB = 1_048_576;
  const off = inState('disabled') as Pick<ResourceInstance, 'status'>;
  const service = (budgetMiB: number) =>
    rowOf(
      {},
      {
        enabled: false,
        ...inState('disabled'),
        status: {
          ...off.status,
          memory: {
            budget_bytes: budgetMiB * MIB,
            budget_source: 'author',
            held_until_restart: false,
            near_budget: false,
            on_over_memory: 'stop',
            over_budget: false,
            failures_in_window: 0,
            switch_off_failures: 3,
            switch_off_window_seconds: 3600,
            switch_off_source: 'default',
          },
        },
      },
    );
  const poolOf = (committedMiB: number) =>
    catalogueOf({
      memoryPool: {
        committed_bytes: committedMiB * MIB,
        pool_bytes: 3072 * MIB,
        source: 'detected',
      },
    });

  // A release that requires another extension, while its plan is read and
  // once it is: the plan adds one extension that listens on a port.
  const needsReader = rowOf({
    requires: [{ module: 'com.github.aux.acme.reader', version: '^1.2' }],
  });
  const reading = catalogueOf({
    planOf: () => ({ state: 'loading' }),
    readPlan: () => new Promise(() => undefined),
  });
  const plannedWithPort = {
    state: 'ready',
    dependencies: [
      {
        module: 'github.com/acme/reader',
        name: 'reader',
        title: 'Reader',
        version: '1.2.0',
        source: '/srv/modules/reader',
        installed: false,
        requests_known: true,
        namespace: 'com.github.aux.acme.reader',
        permissions: ['read_storage'],
        listen: [{ name: 'feed', network: 'tcp', port: 9100, why: 'the feed' }],
        required_by: ['com.github.prusa3d.spool-tracker ^1.2'],
      },
    ],
  };
  const withPlanPorts = catalogueOf({
    planOf: () => plannedWithPort,
    readPlan: () => Promise.resolve(plannedWithPort),
  });

  // The marketplace's three marks, and an official release whose plan adds
  // a community extension.
  const official = rowOf({ trust: 'official' });
  const verified = rowOf({
    title: 'Parts',
    author: 'Acme',
    trust: 'verified',
  });
  const community = rowOf({
    title: 'Nozzle watch',
    author: 'Jane Doe',
    trust: 'community',
  });
  const plannedCommunity = {
    ...plannedWithPort,
    dependencies: plannedWithPort.dependencies.map((one) => ({
      ...one,
      listen: [],
      origin: 'marketplace',
      author: 'Jane Doe',
      trust: 'community',
    })),
  };
  const withCommunityPlan = catalogueOf({
    planOf: () => plannedCommunity,
    readPlan: () => Promise.resolve(plannedCommunity),
  });
</script>

<!--
  The dialog is a modal opened over the page, so each story opens it at once.
  Left out: the dialog while an act runs, which closes it.
-->

<Story name="Switch On" asChild>
  <ExtensionDialog
    catalogue={poolOf(1024)}
    asking={{ row: service(256), act: 'enable' }}
    rows={[service(256)]}
    onclose={noop}
  />
</Story>

<Story name="Switch On Over The Pool" asChild>
  <ExtensionDialog
    catalogue={poolOf(1024)}
    asking={{ row: service(2560), act: 'enable' }}
    rows={[service(2560)]}
    onclose={noop}
  />
</Story>

<Story name="Install Over The Pool" asChild>
  <ExtensionDialog
    catalogue={poolOf(3000)}
    asking={{ row: known, act: 'install' }}
    {rows}
    onclose={noop}
  />
</Story>

<Story name="Install" asChild>
  <ExtensionDialog
    {catalogue}
    asking={{ row: known, act: 'install' }}
    {rows}
    onclose={noop}
  />
</Story>

<!-- A plan taller than the window: the body scrolls, and the title and the
     buttons stay in view. -->
<Story name="Install With A Long Plan" asChild>
  <ExtensionDialog
    catalogue={catalogueOf({ planOf: () => longPlan })}
    asking={{
      row: rowOf({ requires: [{ module: 'x', version: '^1' }] }),
      act: 'install',
    }}
    {rows}
    onclose={noop}
  />
</Story>

<Story name="Install An Official Extension" asChild>
  <ExtensionDialog
    {catalogue}
    asking={{ row: official, act: 'install' }}
    rows={[official]}
    onclose={noop}
  />
</Story>

<Story name="Install A Verified Extension" asChild>
  <ExtensionDialog
    {catalogue}
    asking={{ row: verified, act: 'install' }}
    rows={[verified]}
    onclose={noop}
  />
</Story>

<!-- Install stays disabled until the person ticks that they accept the risk. -->
<Story name="Install A Community Extension" asChild>
  <ExtensionDialog
    {catalogue}
    asking={{ row: community, act: 'install' }}
    rows={[community]}
    onclose={noop}
  />
</Story>

<Story name="Install An Official Extension That Adds A Community One" asChild>
  <ExtensionDialog
    catalogue={withCommunityPlan}
    asking={{
      row: rowOf({
        trust: 'official',
        requires: [{ module: 'com.github.aux.acme.reader', version: '^1.2' }],
      }),
      act: 'install',
    }}
    {rows}
    onclose={noop}
  />
</Story>

<Story name="Install A Beta Release" asChild>
  <ExtensionDialog
    {catalogue}
    asking={{ row: rowOf({ channel: 'beta' }), act: 'install' }}
    {rows}
    onclose={noop}
  />
</Story>

<Story name="Install A Release The Row Cannot List" asChild>
  <ExtensionDialog
    {catalogue}
    asking={{ row: unread, act: 'install' }}
    {rows}
    onclose={noop}
  />
</Story>

<Story name="Install With A Port" asChild>
  <ExtensionDialog
    {catalogue}
    asking={{ row: withPorts, act: 'install' }}
    {rows}
    onclose={noop}
  />
</Story>

<Story name="Install While The Plan Is Read" asChild>
  <ExtensionDialog
    catalogue={reading}
    asking={{ row: needsReader, act: 'install' }}
    {rows}
    onclose={noop}
  />
</Story>

<Story name="Install With The Port Of An Extension The Plan Adds" asChild>
  <ExtensionDialog
    catalogue={withPlanPorts}
    asking={{ row: needsReader, act: 'install' }}
    {rows}
    onclose={noop}
  />
</Story>

<Story name="Install With A Database" asChild>
  <ExtensionDialog
    {catalogue}
    asking={{ row: withDatabase, act: 'install' }}
    {rows}
    onclose={noop}
  />
</Story>

<Story name="Update" asChild>
  <ExtensionDialog
    {catalogue}
    asking={{ row: installed, act: 'update' }}
    rows={[installed]}
    onclose={noop}
  />
</Story>

<Story name="Install Another Release Than The One Offered" asChild>
  <ExtensionDialog
    {catalogue}
    asking={{ row: known, act: 'install', version: '1.3.0' }}
    {rows}
    onclose={noop}
  />
</Story>

<Story name="Remove" asChild>
  <ExtensionDialog
    {catalogue}
    asking={{ row: installed, act: 'remove' }}
    rows={[installed]}
    onclose={noop}
  />
</Story>

<Story name="Remove While Modules Are Off" asChild>
  <ExtensionDialog
    catalogue={catalogueOf({ modulesOn: false })}
    asking={{ row: installed, act: 'remove' }}
    rows={[installed]}
    onclose={noop}
  />
</Story>

<Story name="Approve What The Server Read" asChild>
  <ExtensionDialog
    catalogue={catalogueOf({ pending: pendingOf('unread') })}
    asking={undefined}
    {rows}
    onclose={noop}
  />
</Story>

<Story name="Approve A Release That Asks For More" asChild>
  <ExtensionDialog
    catalogue={catalogueOf({
      pending: pendingOf('changed', ['permission jobs.write']),
    })}
    asking={undefined}
    {rows}
    onclose={noop}
  />
</Story>
