<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import NavTabs from './nav-tabs.svelte';

  const { Story } = defineMeta({
    title: 'Primitives/Nav Tabs',
    component: NavTabs,
  });

  const tabs = [
    { id: 'general', href: '#general', label: 'General' },
    { id: 'network', href: '#network', label: 'Network' },
    { id: 'modules', href: '#modules', label: 'Modules' },
  ];
</script>

<Story name="Default" asChild>
  <NavTabs label="Settings" {tabs} active="network" />
</Story>

<!-- More tabs than the strip is wide: it scrolls, fades its edges, and brings
     the open tab into view. -->
<Story name="Overflowing" asChild>
  <div class="w-64">
    <NavTabs
      label="Settings"
      active="backups"
      tabs={[
        ...tabs,
        { id: 'accounts', href: '#accounts', label: 'Accounts' },
        { id: 'backups', href: '#backups', label: 'Backups' },
        { id: 'about', href: '#about', label: 'About' },
      ]}
    />
  </div>
</Story>
