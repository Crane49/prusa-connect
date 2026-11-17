<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { noop } from '../../../../test/extension-fixtures';
  import ExtensionDatabase from './extension-database.svelte';

  const { Story } = defineMeta({
    title: 'Parts/Extensions/Extension Database',
    component: ExtensionDatabase,
  });
</script>

<Story name="Install On A SQLite File" asChild>
  <ExtensionDatabase mode="sqlite" update={false} onpick={noop} />
</Story>

<Story name="Install On A Postgres Database You Made" asChild>
  <ExtensionDatabase
    mode="postgres"
    dsn="postgres://notes@db.lan:5432/module_notes"
    update={false}
    onpick={noop}
  />
</Story>

<Story name="Postgres With A Connection String That Is Not One" asChild>
  <ExtensionDatabase
    mode="postgres"
    dsn="db.lan"
    update={false}
    onpick={noop}
  />
</Story>

<Story name="Install On A New Database" asChild>
  <ExtensionDatabase mode="provision" update={false} onpick={noop} />
</Story>

<Story name="Update Keeps The SQLite File" asChild>
  <ExtensionDatabase
    mode="keep"
    update
    current={{ engine: 'sqlite' }}
    onpick={noop}
  />
</Story>

<Story name="Update Keeps A Postgres Database" asChild>
  <ExtensionDatabase
    mode="keep"
    update
    current={{ engine: 'postgres', server: 'db.lan:5432/module_notes' }}
    onpick={noop}
  />
</Story>

<Story name="Update Keeps A Database Made For It" asChild>
  <ExtensionDatabase
    mode="keep"
    update
    current={{
      engine: 'postgres',
      provisioned: true,
      server: 'db.lan:5432/module_notes',
    }}
    onpick={noop}
  />
</Story>
