<script lang="ts">
  import { accountIdOf } from '@prusa/connect-ui/data';
  import {
    SettingsAccountScreen,
    SettingsScreen,
  } from '@prusa/connect-ui/shell';

  import type { AppContext } from '../screens';

  let { ctx }: { ctx: AppContext } = $props();

  const session = $derived(ctx.session);
  const accounts = $derived(ctx.accounts);

  $effect(() => {
    void accounts.load();
  });

  const mine = $derived(
    accounts.items.find((row) => row.id === accountIdOf(session.principal?.id)),
  );

  // The list holds only accounts with a password here, and only an
  // administrator may read it. A person with no row is named by the session.
  const identity = $derived(
    mine === undefined
      ? { displayName: session.principal?.name }
      : {
          username: mine.username,
          displayName: mine.display_name,
          role: mine.role,
          createdAt: mine.created_at,
        },
  );
</script>

<SettingsScreen>
  <SettingsAccountScreen
    source={session}
    changing={session.changing}
    changed={session.changed}
    fault={session.passwordFault}
    {identity}
    identityLoading={accounts.loading}
    passwordless={session.redirectProvider !== undefined}
    onChangePassword={(current: string, next: string) => {
      void session.changePassword(current, next);
    }}
  />
</SettingsScreen>
