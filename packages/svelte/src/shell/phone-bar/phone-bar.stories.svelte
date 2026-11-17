<script module lang="ts">
  import ListChecksIcon from '@lucide/svelte/icons/list-checks';
  import { PrinterGlyphIcon } from '../../primitives';
  import SettingsIcon from '@lucide/svelte/icons/settings';
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import type { NavItem } from '../../slots';
  import type { RailNavItem } from '../rail';
  import PhoneBar from './phone-bar.svelte';

  const { Story } = defineMeta({
    title: 'Shell/PhoneBar',
    component: PhoneBar,
  });

  const items: NavItem[] = [
    {
      id: 'printers',
      label: 'Printers',
      href: '/printers',
      icon: PrinterGlyphIcon,
    },
    { id: 'jobs', label: 'Jobs', href: '/jobs', icon: ListChecksIcon },
    {
      id: 'settings',
      label: 'Settings',
      href: '/settings',
      icon: SettingsIcon,
    },
  ];

  // A module with several pages, drawn under its own name in the open menu.
  const grouped: RailNavItem[] = [
    ...items,
    {
      id: 'farm',
      label: 'Farm',
      href: '/m/farm/plan',
      icon: ListChecksIcon,
      children: [
        { id: 'plan', label: 'Plan', href: '/m/farm/plan' },
        { id: 'farm-jobs', label: 'Jobs', href: '/m/farm/jobs' },
      ],
    },
  ];
</script>

<Story
  name="Online"
  args={{
    productName: 'Prusa Connect',
    items,
    currentPath: '/printers',
    connection: 'online',
    principal: 'user:alice',
  }}
/>

<Story
  name="Offline"
  args={{
    productName: 'Prusa Connect',
    items,
    currentPath: '/printers',
    connection: 'offline',
  }}
/>

<Story
  name="With a module group"
  args={{
    productName: 'Prusa Connect',
    items: grouped,
    currentPath: '/m/farm/jobs',
    connection: 'online',
  }}
/>
