<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import { Checkbox, Label } from '../../primitives';
  import ExtensionDetailScreen from './extension-detail-screen.svelte';
  import type { ExtensionDetailScreenProps } from './types';

  const { Story } = defineMeta({
    title: 'Parts/Screen components/Extension Detail',
    component: ExtensionDetailScreen,
  });

  const base: ExtensionDetailScreenProps = {
    title: 'Spools',
    version: '1.2.0',
    source: 'Local',
    module: 'github.com/prusa3d/spools',
    observed: 'running',
    tabs: [
      { id: 'settings', label: 'Settings' },
      { id: 'permissions', label: 'Permissions' },
    ],
    tab: 'settings',
    onTabChange: () => undefined,
    extensionsHref: '/settings/extensions',
  };
</script>

{#snippet control()}
  <Label>
    <Checkbox checked /> On
  </Label>
{/snippet}

<Story name="Running" asChild>
  <ExtensionDetailScreen {...base} {control} />
</Story>

<Story name="Stopped" asChild>
  <ExtensionDetailScreen {...base} observed="stopped" />
</Story>

<Story name="Failed" asChild>
  <ExtensionDetailScreen
    {...base}
    observed="failed"
    result="The module exited with status 2 while it started."
  />
</Story>

<Story name="Degraded" asChild>
  <ExtensionDetailScreen
    {...base}
    observed="degraded"
    result="The module's breaker is open."
  />
</Story>

<Story name="Retrying" asChild>
  <ExtensionDetailScreen
    {...base}
    observed="retrying"
    result="Hung and was restarted"
  />
</Story>

<Story name="Switched Off" asChild>
  <ExtensionDetailScreen
    {...base}
    observed="switched-off"
    result="Switched off after 3 failures within 1 hour"
  />
</Story>

<Story name="Loading" asChild>
  <ExtensionDetailScreen {...base} loading />
</Story>

<Story name="Many Module Tabs" asChild>
  <ExtensionDetailScreen
    {...base}
    tabs={[
      ...base.tabs,
      { id: 'a.one', label: 'Shelf layout' },
      { id: 'a.two', label: 'Spool labels' },
      { id: 'a.three', label: 'Reorder rules' },
      { id: 'a.four', label: 'Import and export' },
    ]}
  />
</Story>
