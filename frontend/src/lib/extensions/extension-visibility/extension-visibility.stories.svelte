<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { instanceOf } from '../../../../test/extension-fixtures';
  import ExtensionVisibility from './extension-visibility.svelte';

  const { Story } = defineMeta({
    title: 'Parts/Extensions/Extension Visibility',
    component: ExtensionVisibility,
  });

  const instance = instanceOf({
    name: 'spools',
    hidden_contributions: ['device_card/shelf'],
  });
  const contributions = [
    { key: 'pages/spools', title: 'Spools', kind: 'page' },
    { key: 'device_card/shelf', title: 'Shelf line', kind: 'deviceCard' },
  ] as const;
  const onhide = (): Promise<boolean> => Promise.resolve(true);
</script>

<Story name="Shown And Hidden" asChild>
  <ExtensionVisibility
    {instance}
    {contributions}
    saving={false}
    conflict={false}
    {onhide}
  />
</Story>

<Story name="Locked" asChild>
  <ExtensionVisibility
    {instance}
    {contributions}
    locked="This module is configured in a file."
    saving={false}
    conflict={false}
    {onhide}
  />
</Story>

<Story name="Conflict" asChild>
  <ExtensionVisibility
    {instance}
    {contributions}
    saving={false}
    conflict
    {onhide}
  />
</Story>
