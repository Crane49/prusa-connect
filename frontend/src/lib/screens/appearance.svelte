<script lang="ts">
  import {
    SettingsAppearanceScreen,
    SettingsScreen,
  } from '@prusa/connect-ui/shell';

  import type { AppContext } from '../screens';

  let { ctx }: { ctx: AppContext } = $props();
</script>

<SettingsScreen>
  <SettingsAppearanceScreen
    home={ctx.preferences}
    dashboards={ctx.settings.dashboards}
  />
</SettingsScreen>
