<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import {
    catalogueOf,
    inState,
    noop,
    rowOf,
  } from '../../../../test/extension-fixtures';
  import ExtensionActions from './extension-actions.svelte';

  const { Story } = defineMeta({
    title: 'Parts/Extensions/Extension Actions',
    component: ExtensionActions,
  });

  const catalogue = catalogueOf();
  const pages = [
    {
      title: 'Spools',
      href: '/m/com.github.prusa3d.spool-tracker/spools',
    },
  ];
</script>

<!--
  The row menu stays closed until a person opens it, so the stories show the
  buttons and the switch. A row the server offers from the marketplace opens
  its page in a tab of its own; a page the module adds opens in place.
-->

<Story name="Available From The Marketplace" asChild>
  <ExtensionActions
    row={rowOf({ page_url: 'https://marketplace.example/spool-tracker' })}
    {catalogue}
    go={noop}
    ask={noop}
  />
</Story>

<Story name="Available From This Server" asChild>
  <ExtensionActions
    row={rowOf({ origin: 'local' })}
    {catalogue}
    go={noop}
    ask={noop}
  />
</Story>

<Story name="Installing" asChild>
  <ExtensionActions
    row={rowOf()}
    catalogue={catalogueOf({
      acting: 'github.com/prusa3d/spool-tracker',
    })}
    go={noop}
    ask={noop}
  />
</Story>

<Story name="Update Offered" asChild>
  <ExtensionActions
    row={rowOf({ version: '1.5.0' }, {})}
    {catalogue}
    go={noop}
    ask={noop}
  />
</Story>

<Story name="Installed With A Page" asChild>
  <ExtensionActions
    row={{ ...rowOf({}, {}), pages }}
    {catalogue}
    go={noop}
    ask={noop}
  />
</Story>

<Story name="Installed And Stopped" asChild>
  <ExtensionActions
    row={rowOf({}, inState('disabled'))}
    {catalogue}
    go={noop}
    ask={noop}
  />
</Story>

<Story name="Declared In The Configuration File" asChild>
  <ExtensionActions
    row={rowOf({}, { source: 'file' })}
    {catalogue}
    go={noop}
    ask={noop}
  />
</Story>

<Story name="Without Access" asChild>
  <ExtensionActions
    row={rowOf({ origin: 'local' })}
    catalogue={catalogueOf({ mayConfigure: () => false })}
    go={noop}
    ask={noop}
  />
</Story>
