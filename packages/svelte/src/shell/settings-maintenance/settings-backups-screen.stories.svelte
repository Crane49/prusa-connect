<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import SettingsBackupsScreen from './settings-backups-screen.svelte';
  import { maintenanceSource } from './story-source';

  const { Story } = defineMeta({
    title: 'Parts/Screen components/Settings Backups',
    component: SettingsBackupsScreen,
  });
</script>

<Story name="Empty" asChild>
  <SettingsBackupsScreen source={maintenanceSource()} />
</Story>

<Story name="With Backup" asChild>
  <SettingsBackupsScreen
    source={maintenanceSource({
      backups: [
        {
          created_at: '2026-10-26T12:00:00Z',
          files: [],
          format: 1,
          from_version: '1.2.0',
          id: 'backup-20261026T120000Z',
        },
      ],
    })}
  />
</Story>
