<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import {
    catalogueOf,
    inState,
    noop,
    rowOf,
  } from '../../../../test/extension-fixtures';
  import ExtensionSwitch from './extension-switch.svelte';

  const { Story } = defineMeta({
    title: 'Parts/Extensions/Extension Switch',
    component: ExtensionSwitch,
  });

  const catalogue = catalogueOf();
  const local = { origin: 'local' as const };
</script>

<!--
  Left out: the busy state of a switch being written (catalogue.writing), which
  is a store value that only a running write holds.
-->

<Story name="Install Through The Switch" asChild>
  <ExtensionSwitch row={rowOf(local)} {catalogue} ask={noop} />
</Story>

<Story name="On" asChild>
  <ExtensionSwitch row={rowOf({}, {})} {catalogue} ask={noop} />
</Story>

<Story name="Off" asChild>
  <ExtensionSwitch
    row={rowOf({}, inState('disabled'))}
    {catalogue}
    ask={noop}
  />
</Story>

<Story name="Starting" asChild>
  <ExtensionSwitch
    row={rowOf({}, inState('starting'))}
    {catalogue}
    ask={noop}
  />
</Story>

<Story name="Failed" asChild>
  <ExtensionSwitch row={rowOf({}, inState('failed'))} {catalogue} ask={noop} />
</Story>

<Story name="Installed Without Approval" asChild>
  <ExtensionSwitch
    row={rowOf({}, { module: { title: 'Spool tracker', version: '1.4.0' } })}
    {catalogue}
    ask={noop}
  />
</Story>

<Story name="Locked By The Configuration File" asChild>
  <ExtensionSwitch row={rowOf({}, { source: 'file' })} {catalogue} ask={noop} />
</Story>

<Story name="Locked Without Access" asChild>
  <ExtensionSwitch
    row={rowOf({}, {})}
    catalogue={catalogueOf({ mayConfigure: () => false })}
    ask={noop}
  />
</Story>

<Story name="Locked While Modules Are Off" asChild>
  <ExtensionSwitch
    row={rowOf({}, {})}
    catalogue={catalogueOf({ modulesOn: false })}
    ask={noop}
  />
</Story>

<Story name="Waiting For Another Act" asChild>
  <ExtensionSwitch
    row={rowOf({}, {})}
    catalogue={catalogueOf({ acting: 'github.com/prusa3d/other' })}
    ask={noop}
  />
</Story>
