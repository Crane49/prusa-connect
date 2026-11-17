<script lang="ts">
  import SettingsBackupsScreen from './settings-backups-screen.svelte';
  import SettingsUpdatesScreen from './settings-updates-screen.svelte';
  import type { SettingsMaintenanceScreenProps } from './types';

  let { source }: SettingsMaintenanceScreenProps = $props();
</script>

<div class="flex w-full flex-col gap-4">
  <SettingsUpdatesScreen {source} />
  <SettingsBackupsScreen {source} />
</div>
