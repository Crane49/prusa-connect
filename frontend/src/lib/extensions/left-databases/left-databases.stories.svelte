<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import {
    LeftDatabases,
    type LeftDatabase,
    type LeftDatabasesClient,
  } from '../../stores/left-databases.svelte';
  import LeftDatabasesList from './left-databases.svelte';

  const { Story } = defineMeta({
    title: 'Parts/Extensions/Left Databases',
    component: LeftDatabasesList,
  });

  const left = (over: Partial<LeftDatabase>): LeftDatabase => ({
    engine: 'sqlite',
    instance: 'driver:spool-tracker',
    left_at: '2026-10-14T09:00:00Z',
    purge: 'delete',
    type: 'module',
    where: '/var/lib/prusa-connect/spool-tracker.db',
    why: 'removed',
    ...over,
  });

  const storeOf = (items: readonly LeftDatabase[]) =>
    new LeftDatabases({
      listModuleDatabases: () => Promise.resolve({ items }),
    } as unknown as LeftDatabasesClient);
</script>

<!--
  Left out: the empty state, which draws nothing, the purge result and a refused purge, which only a purge a person
  confirmed produces, and the dialog, which opens on a click.
-->

<Story name="Removed Module With A SQLite File" asChild>
  <LeftDatabasesList store={storeOf([left({})])} />
</Story>

<Story name="Postgres Database Kept" asChild>
  <LeftDatabasesList
    store={storeOf([
      left({
        engine: 'postgres',
        purge: 'forget',
        where: 'db.lan:5432/module_notes',
        why: 'moved',
      }),
    ])}
  />
</Story>

<Story name="Database Made For The Module" asChild>
  <LeftDatabasesList
    store={storeOf([
      left({
        engine: 'postgres',
        provisioned: true,
        purge: 'drop',
        where: 'db.lan:5432/module_spool',
        why: 'not_granted',
      }),
    ])}
  />
</Story>

<Story name="Several Instances" asChild>
  <LeftDatabasesList
    store={storeOf([
      left({}),
      left({
        instance: 'driver:printer-notes',
        where: '/var/lib/prusa-connect/printer-notes.db',
        why: 'moved',
      }),
    ])}
  />
</Story>
