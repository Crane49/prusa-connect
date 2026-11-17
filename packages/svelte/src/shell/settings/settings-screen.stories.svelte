<script module lang="ts">
  import { defineMeta } from '@storybook/addon-svelte-csf';

  import SettingsScreen from './settings-screen.svelte';

  const { Story } = defineMeta({
    title: 'Parts/Screen components/Settings',
    component: SettingsScreen,
  });
</script>

<!-- The chrome with a stand-in for the pane. -->
<Story name="Default" asChild>
  <SettingsScreen>
    <section class="rounded-panel bg-card p-6 text-card-foreground">
      <h2 class="text-lead font-semibold">Account</h2>
    </section>
  </SettingsScreen>
</Story>

<!-- What a host draws while it is still mounting the pane the path names. -->
<Story name="A Pane On Its Way" asChild>
  <SettingsScreen />
</Story>
