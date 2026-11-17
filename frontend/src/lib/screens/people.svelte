<script lang="ts">
  import {
    SettingsPeopleScreen,
    SettingsScreen,
  } from '@prusa/connect-ui/shell';

  import { personHref } from '../routes';
  import type { AppContext } from '../screens';

  let { ctx }: { ctx: AppContext } = $props();

  const people = $derived(ctx.accounts);

  $effect(() => {
    void people.load();
  });
</script>

<SettingsScreen>
  <SettingsPeopleScreen
    {people}
    subject={ctx.session.principal?.id}
    {personHref}
  />
</SettingsScreen>
